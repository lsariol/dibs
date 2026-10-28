package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// client is one browser: its own cookie jar, so its own session.
type client struct {
	t    *testing.T
	base string
	http *http.Client
	code string
}

func newClient(t *testing.T, base string) *client {
	jar, _ := cookiejar.New(nil)
	return &client{t: t, base: base, http: &http.Client{
		Jar:           jar,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

// post submits a form the way a browser on the same site does.
func (c *client) post(path string, form url.Values) *http.Response {
	c.t.Helper()
	req, _ := http.NewRequest("POST", c.base+path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", c.base)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	resp.Body.Close()
	return resp
}

func (c *client) create(name string, extra url.Values) {
	c.t.Helper()
	form := url.Values{"name": {name}, "preset": {"blank"}, "maxPlayers": {"8"}, "scoreMode": {"owner"}}
	for k, v := range extra {
		form[k] = v
	}
	resp := c.post("/new", form)
	if resp.StatusCode != http.StatusSeeOther {
		c.t.Fatalf("create: status %d", resp.StatusCode)
	}
	c.code = strings.TrimPrefix(resp.Header.Get("Location"), "/l/")
}

func (c *client) join(code, name string) {
	c.t.Helper()
	resp := c.post("/join", url.Values{"code": {strings.ToLower(code)}, "name": {name}})
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/l/"+code {
		c.t.Fatalf("join: status %d location %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	c.code = code
}

// ws is a connected player.
type ws struct {
	t     *testing.T
	conn  *websocket.Conn
	state map[string]any
	n     int
	raw   []string // every message received, as sent
}

func (c *client) dial() *ws {
	c.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	u := "ws" + strings.TrimPrefix(c.base, "http") + "/l/" + c.code + "/ws"
	conn, _, err := websocket.Dial(ctx, u, &websocket.DialOptions{HTTPClient: c.http, HTTPHeader: http.Header{"Origin": {c.base}}})
	if err != nil {
		c.t.Fatalf("dial: %v", err)
	}
	conn.SetReadLimit(1 << 20)
	w := &ws{t: c.t, conn: conn}
	c.t.Cleanup(func() { conn.CloseNow() })
	w.waitState(func(map[string]any) bool { return true })
	return w
}

// read returns the next message.
func (w *ws) read() map[string]any {
	w.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, data, err := w.conn.Read(ctx)
	if err != nil {
		w.t.Fatalf("read: %v", err)
	}
	w.raw = append(w.raw, string(data))
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		w.t.Fatal(err)
	}
	if m["type"] == "state" {
		w.state = m
	}
	return m
}

// waitState returns the latest state if it satisfies ok, else reads until one does.
func (w *ws) waitState(ok func(map[string]any) bool) map[string]any {
	w.t.Helper()
	if w.state != nil && ok(w.state) {
		return w.state
	}
	for range 200 {
		m := w.read()
		if m["type"] == "state" && ok(m) {
			return m
		}
	}
	w.t.Fatal("state never matched")
	return nil
}

// do sends an action and returns its result (skipping states on the way).
func (w *ws) do(action string, params any) map[string]any {
	w.t.Helper()
	w.n++
	id := fmt.Sprintf("q%d", w.n)
	b, _ := json.Marshal(map[string]any{"id": id, "rev": w.rev(), "action": action, "params": params})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := w.conn.Write(ctx, websocket.MessageText, b); err != nil {
		w.t.Fatal(err)
	}
	for range 200 {
		m := w.read()
		if m["type"] == "result" && m["id"] == id {
			return m
		}
	}
	w.t.Fatal("no result")
	return nil
}

func (w *ws) ok(action string, params any) {
	w.t.Helper()
	if r := w.do(action, params); r["ok"] != true {
		w.t.Fatalf("%s failed: %v", action, r["error"])
	}
}

func (w *ws) rev() float64 {
	if w.state == nil {
		return 0
	}
	return w.state["rev"].(float64)
}

func players(state map[string]any) []any {
	return state["lobby"].(map[string]any)["players"].([]any)
}

func TestLobbyFlowOverWebSocket(t *testing.T) {
	ts := newTestServer(t)
	ann := newClient(t, ts.URL)
	ann.create("Ann", nil)
	bob := newClient(t, ts.URL)
	bob.join(ann.code, "Bob")

	a := ann.dial()
	b := bob.dial()
	if b.state["you"] == a.state["you"] {
		t.Fatal("two browsers got the same player")
	}
	a.waitState(func(s map[string]any) bool { return len(players(s)) == 2 })

	b.ok("lobby.rename", map[string]any{"name": "Robert"})
	a.waitState(func(s map[string]any) bool {
		return players(s)[1].(map[string]any)["name"] == "Robert"
	})

	// Refresh: a new connection with the same cookie is the same player, same seat.
	bobID := b.state["you"]
	b.conn.Close(websocket.StatusNormalClosure, "")
	a.waitState(func(s map[string]any) bool { return players(s)[1].(map[string]any)["connected"] == false })
	b2 := bob.dial()
	if b2.state["you"] != bobID || players(b2.state)[1].(map[string]any)["seat"] != float64(1) {
		t.Fatal("reconnect didn't restore the same player and seat")
	}
	a.waitState(func(s map[string]any) bool { return players(s)[1].(map[string]any)["connected"] == true })
}

func TestShareURLAndJoinPage(t *testing.T) {
	ts := newTestServer(t)
	ann := newClient(t, ts.URL)
	ann.create("Ann", nil)

	cy := newClient(t, ts.URL)
	resp, err := cy.http.Get(ts.URL + "/l/" + ann.code)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("share link: %d", resp.StatusCode)
	}
	r := cy.post("/l/"+ann.code+"/join", url.Values{"name": {"Cy"}})
	if r.StatusCode != http.StatusSeeOther {
		t.Fatalf("join from link: %d", r.StatusCode)
	}
	for _, c := range r.Cookies() {
		if c.Name == sessionCookie && (!c.HttpOnly || c.Path != "/l/"+ann.code || c.SameSite != http.SameSiteLaxMode) {
			t.Fatalf("session cookie flags: %+v", c)
		}
	}
	if strings.Contains(r.Header.Get("Location"), "token") || strings.Contains(r.Header.Get("Location"), "?") {
		t.Fatal("credential in redirect URL")
	}
}

func TestWebSocketNeedsSession(t *testing.T) {
	ts := newTestServer(t)
	ann := newClient(t, ts.URL)
	ann.create("Ann", nil)
	eve := newClient(t, ts.URL)
	eve.code = ann.code
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(ts.URL, "http")+"/l/"+ann.code+"/ws", &websocket.DialOptions{HTTPClient: eve.http})
	if err == nil || resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("no session: err %v", err)
	}
	if r, _ := eve.http.Get(ts.URL + "/l/" + ann.code + "/me"); r.StatusCode != http.StatusForbidden {
		t.Fatalf("/me without session: %d", r.StatusCode)
	}
}

func TestWebSocketRejectsForeignOrigin(t *testing.T) {
	ts := newTestServer(t)
	ann := newClient(t, ts.URL)
	ann.create("Ann", nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(ts.URL, "http")+"/l/"+ann.code+"/ws",
		&websocket.DialOptions{HTTPClient: ann.http, HTTPHeader: http.Header{"Origin": {"https://evil.example"}}})
	if err == nil || resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("foreign origin accepted: %v", err)
	}
}

func TestCrossSiteFormRejected(t *testing.T) {
	ts := newTestServer(t)
	c := newClient(t, ts.URL)
	req, _ := http.NewRequest("POST", ts.URL+"/new", strings.NewReader("name=Eve"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	resp, err := c.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-site POST: %d", resp.StatusCode)
	}
}

func TestUnknownCodesAreRateLimited(t *testing.T) {
	ts := newTestServer(t)
	ann := newClient(t, ts.URL)
	ann.create("Ann", nil)
	eve := newClient(t, ts.URL)
	for i := range 30 {
		eve.post("/join", url.Values{"code": {fmt.Sprintf("ZZZZ%02d", i)}, "name": {"Eve"}})
	}
	// Once limited, even a real code looks unknown.
	r := eve.post("/join", url.Values{"code": {ann.code}, "name": {"Eve"}})
	if r.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("guessing wasn't limited: %d", r.StatusCode)
	}
	resp, _ := eve.http.Get(ts.URL + "/l/" + ann.code)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("limited client can still find a lobby: %d", resp.StatusCode)
	}
}

func TestMalformedMessagesDontBreakTheConnection(t *testing.T) {
	ts := newTestServer(t)
	ann := newClient(t, ts.URL)
	ann.create("Ann", nil)
	a := ann.dial()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, junk := range []string{"not json", `{"id":1}`, `{"id":"x","action":"lobby.seat","params":"bad"}`, `[]`} {
		if err := a.conn.Write(ctx, websocket.MessageText, []byte(junk)); err != nil {
			t.Fatal(err)
		}
		for {
			m := a.read()
			if m["type"] == "result" {
				if m["ok"] == true {
					t.Fatalf("%q succeeded", junk)
				}
				break
			}
		}
	}
	a.ok("lobby.ready", map[string]any{"ready": true})
}

func TestHealthCounts(t *testing.T) {
	ts := newTestServer(t)
	ann := newClient(t, ts.URL)
	ann.create("Ann", nil)
	ann.dial()
	_, body := get(t, ts.URL+"/health")
	if !strings.Contains(body, `"lobbies":1`) {
		t.Fatalf("health: %s", body)
	}
}
