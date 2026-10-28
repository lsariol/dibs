package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/lsariol/dibs/internal/lobby"
)

// WebSocket limits.
const (
	wsReadLimit    = 16 << 10
	wsWriteTimeout = 10 * time.Second
	wsPingEvery    = 25 * time.Second // well inside Cloudflare's 100 s idle timeout
	wsResultQueue  = 32
)

// handleWS upgrades an authenticated player to the realtime connection.
func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	ip := s.clientIP(r)
	if !s.wsLimit.allow(ip) {
		http.Error(w, "Too many connections; wait a moment.", http.StatusTooManyRequests)
		return
	}
	code := lobby.NormalizeCode(r.PathValue("code"))
	l := s.lobbies.Get(code)
	if l == nil {
		http.Error(w, "No such lobby.", http.StatusNotFound)
		return
	}
	playerID, ok := l.Authenticate(s.sessionToken(r, code))
	if !ok {
		http.Error(w, "Join the lobby first.", http.StatusForbidden)
		return
	}
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{s.cfg.PublicURL.Host}})
	if err != nil {
		return
	}
	ws.SetReadLimit(wsReadLimit)

	c := &conn{ws: ws, stateReady: make(chan struct{}, 1), results: make(chan []byte, wsResultQueue), done: make(chan struct{})}
	if err := l.Attach(playerID, c); err != nil {
		b, _ := json.Marshal(map[string]string{"type": "ended", "reason": err.Error()})
		_ = ws.Write(r.Context(), websocket.MessageText, b)
		ws.Close(websocket.StatusPolicyViolation, "refused")
		return
	}
	defer l.Detach(playerID, c)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	go c.writeLoop(ctx, cancel)

	actions := newLimiter(600, 40)
	for {
		_, data, err := ws.Read(ctx)
		if err != nil {
			c.End("")
			return
		}
		var req lobby.Request
		var resp lobby.Response
		if err := json.Unmarshal(data, &req); err != nil {
			resp = lobby.Response{Type: "result", Error: &lobby.Error{Code: lobby.CodeBadRequest, Message: "That message isn't valid JSON."}}
		} else if !actions.allow("") {
			resp = lobby.Response{Type: "result", ID: req.ID, Error: &lobby.Error{Code: lobby.CodeInvalid, Message: "Slow down a little."}}
		} else {
			resp = l.Do(playerID, req)
		}
		b, _ := json.Marshal(resp)
		select {
		case c.results <- b:
		default:
			c.End("too slow")
			return
		}
	}
}

// conn is one player's WebSocket. It implements lobby.Subscriber: the lobby
// hands it snapshots without ever waiting on the network.
type conn struct {
	ws         *websocket.Conn
	mu         sync.Mutex
	latest     []byte // newest unsent snapshot
	stateReady chan struct{}
	results    chan []byte
	done       chan struct{}
	endOnce    sync.Once
	endReason  string
}

// SendState keeps only the newest snapshot, so a slow phone skips stale ones.
func (c *conn) SendState(msg []byte) {
	c.mu.Lock()
	c.latest = msg
	c.mu.Unlock()
	select {
	case c.stateReady <- struct{}{}:
	default:
	}
}

// End closes the connection; reason, if any, is shown to the player.
func (c *conn) End(reason string) {
	c.endOnce.Do(func() {
		c.mu.Lock()
		c.endReason = reason
		c.mu.Unlock()
		close(c.done)
	})
}

func (c *conn) writeLoop(ctx context.Context, cancel context.CancelFunc) {
	defer cancel()
	ping := time.NewTicker(wsPingEvery)
	defer ping.Stop()
	for {
		select {
		case <-c.done:
			c.mu.Lock()
			reason := c.endReason
			c.mu.Unlock()
			if reason != "" && reason != "too slow" {
				b, _ := json.Marshal(map[string]string{"type": "ended", "reason": reason})
				_ = c.write(ctx, b)
				c.ws.Close(websocket.StatusNormalClosure, "ended")
			} else {
				c.ws.Close(websocket.StatusGoingAway, "")
			}
			return
		case <-ctx.Done():
			return
		case <-c.stateReady:
			c.mu.Lock()
			msg := c.latest
			c.latest = nil
			c.mu.Unlock()
			if msg != nil && c.write(ctx, msg) != nil {
				c.End("")
			}
		case b := <-c.results:
			if c.write(ctx, b) != nil {
				c.End("")
			}
		case <-ping.C:
			pctx, pcancel := context.WithTimeout(ctx, wsWriteTimeout)
			err := c.ws.Ping(pctx)
			pcancel()
			if err != nil && !errors.Is(err, context.Canceled) {
				c.End("")
			}
		}
	}
}

func (c *conn) write(ctx context.Context, b []byte) error {
	wctx, cancel := context.WithTimeout(ctx, wsWriteTimeout)
	defer cancel()
	return c.ws.Write(wctx, websocket.MessageText, b)
}
