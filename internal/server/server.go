// Package server is Dibs's HTTP side: pages, static files, health and the
// WebSocket endpoint, with the security headers and limits around them.
package server

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/lsariol/dibs/internal/config"
	"github.com/lsariol/dibs/internal/lobby"
	"github.com/lsariol/dibs/web"
)

// Server serves Dibs.
type Server struct {
	cfg         config.Config
	log         *slog.Logger
	pages       *pages
	mux         *http.ServeMux
	lobbies     *lobby.Manager
	createLimit *limiter // lobbies created per client address
	joinLimit   *limiter // join attempts per client address
	missLimit   *limiter // unknown lobby codes per client address; when empty, every code looks unknown
	wsLimit     *limiter // WebSocket connections per client address
}

// New builds a Server; it fails only if the embedded templates are broken.
func New(cfg config.Config, log *slog.Logger) (*Server, error) {
	p, err := loadPages(cfg.Version)
	if err != nil {
		return nil, err
	}
	s := &Server{
		cfg:         cfg,
		log:         log,
		pages:       p,
		mux:         http.NewServeMux(),
		lobbies:     lobby.NewManager(cfg.PublicURL.String()),
		createLimit: newLimiter(6, 10),
		joinLimit:   newLimiter(20, 20),
		missLimit:   newLimiter(10, 15),
		wsLimit:     newLimiter(60, 30),
	}
	s.routes()
	return s, nil
}

func (s *Server) routes() {
	static, _ := fs.Sub(web.FS, "static")
	s.mux.Handle("GET /static/", http.StripPrefix("/static/", staticHandler(static, s.cfg.Version, s.cfg.Env == "dev")))
	s.mux.HandleFunc("GET /health", s.handleHealth)
	s.mux.HandleFunc("GET /{$}", s.handleLanding)
	s.mux.HandleFunc("GET /new", s.handleNewForm)
	s.mux.HandleFunc("POST /new", s.handleNew)
	s.mux.HandleFunc("GET /join", s.handleJoinForm)
	s.mux.HandleFunc("POST /join", s.handleJoin)
	s.mux.HandleFunc("GET /l/{code}", s.handleLobby)
	s.mux.HandleFunc("POST /l/{code}/join", s.handleJoin)
	s.mux.HandleFunc("GET /l/{code}/ws", s.handleWS)
	s.mux.HandleFunc("GET /l/{code}/me", s.handleMe)
}

// Handler is the full handler chain: security headers, cross-site request
// protection for forms, then the routes.
func (s *Server) Handler() http.Handler {
	csrf := http.NewCrossOriginProtection()
	if err := csrf.AddTrustedOrigin(s.cfg.PublicURL.String()); err != nil {
		s.log.Warn("server: public URL isn't a valid trusted origin", "err", err)
	}
	return securityHeaders(csrf.Handler(s.mux), s.cfg.SecureCookies())
}

// Lobbies is the lobby manager, for tests.
func (s *Server) Lobbies() *lobby.Manager { return s.lobbies }

// Run serves on cfg.Addr until ctx ends, then shuts down gracefully.
func (s *Server) Run(ctx context.Context) error {
	srv := &http.Server{
		Addr:              s.cfg.Addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    16 << 10,
		BaseContext:       func(net.Listener) context.Context { return ctx },
		ErrorLog:          slog.NewLogLogger(s.log.Handler(), slog.LevelWarn),
	}
	ln, err := net.Listen("tcp", s.cfg.Addr)
	if err != nil {
		return fmt.Errorf("server: can't listen on %s: %w (is another Dibs running? set DIBS_ADDR to another port)", s.cfg.Addr, err)
	}
	s.log.Info("Dibs ready", "addr", ln.Addr().String(), "public_url", s.cfg.PublicURL.String())

	go s.lobbies.Run(ctx, func(n int) { s.log.Info("expired lobbies removed", "count", n) })
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	select {
	case err := <-errc:
		return fmt.Errorf("server: %w", err)
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("server: shutdown: %w", err)
	}
	return nil
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	lobbies, connected := s.lobbies.Stats()
	fmt.Fprintf(w, `{"status":"ok","version":%q,"lobbies":%d,"connected":%d}`+"\n", s.cfg.Version, lobbies, connected)
}

func (s *Server) handleLanding(w http.ResponseWriter, r *http.Request) {
	s.render(w, http.StatusOK, "landing", nil)
}

// securityHeaders sets headers every response gets: a strict CSP (no inline
// scripts, same-origin only), no framing, no sniffing, no referrer leaks.
func securityHeaders(next http.Handler, https bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		if https {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

// staticHandler serves embedded files under /static/<version>/…. Links carry
// the running version, so a release busts every cache; files are cached for a
// year only when the version matches (never in dev).
func staticHandler(root fs.FS, version string, dev bool) http.Handler {
	files := http.FileServerFS(root)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ver, rest, ok := strings.Cut(r.URL.Path, "/")
		if !ok || rest == "" || strings.HasSuffix(rest, "/") {
			http.NotFound(w, r)
			return
		}
		if ver == version && !dev {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		r2 := r.Clone(r.Context())
		r2.URL.Path = rest
		files.ServeHTTP(w, r2)
	})
}
