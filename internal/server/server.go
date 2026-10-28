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
	"time"

	"github.com/lsariol/dibs/internal/config"
	"github.com/lsariol/dibs/web"
)

// Server serves Dibs.
type Server struct {
	cfg   config.Config
	log   *slog.Logger
	pages *pages
	mux   *http.ServeMux
}

// New builds a Server; it fails only if the embedded templates are broken.
func New(cfg config.Config, log *slog.Logger) (*Server, error) {
	p, err := loadPages(cfg.Version)
	if err != nil {
		return nil, err
	}
	s := &Server{cfg: cfg, log: log, pages: p, mux: http.NewServeMux()}
	s.routes()
	return s, nil
}

func (s *Server) routes() {
	static, _ := fs.Sub(web.FS, "static")
	s.mux.Handle("GET /static/", http.StripPrefix("/static/", staticHandler(static)))
	s.mux.HandleFunc("GET /health", s.handleHealth)
	s.mux.HandleFunc("GET /{$}", s.handleLanding)
}

// Handler is the full handler chain: security headers, then the routes.
func (s *Server) Handler() http.Handler {
	return securityHeaders(s.mux)
}

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
	fmt.Fprintf(w, `{"status":"ok","version":%q}`+"\n", s.cfg.Version)
}

func (s *Server) handleLanding(w http.ResponseWriter, r *http.Request) {
	s.render(w, http.StatusOK, "landing", nil)
}

// securityHeaders sets headers every response gets: a strict CSP (no inline
// scripts, same-origin only), no framing, no sniffing, no referrer leaks.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

// staticHandler serves embedded files with a cache that the ?v=<version>
// query on every link busts at each release.
func staticHandler(root fs.FS) http.Handler {
	files := http.FileServerFS(root)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "" || r.URL.Path[len(r.URL.Path)-1] == '/' {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=3600")
		files.ServeHTTP(w, r)
	})
}
