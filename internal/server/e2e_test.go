package server

import (
	"net/url"
	"strings"
	"testing"

	"github.com/coder/websocket"
)

// handIDs returns the card IDs a state shows in a container.
func handIDs(state map[string]any, container string) []string {
	var out []string
	for _, c := range state["table"].(map[string]any)["containers"].([]any) {
		cm := c.(map[string]any)
		if cm["id"] != container {
			continue
		}
		for _, cv := range cm["cards"].([]any) {
			if id, ok := cv.(map[string]any)["id"].(string); ok {
				out = append(out, id)
			}
		}
	}
	return out
}

func step(w *ws, name string, args map[string]any) map[string]any {
	w.t.Helper()
	p := map[string]any{"step": name}
	if args != nil {
		p["args"] = args
	}
	return w.do("preset.step", p)
}

// TestFourPhonesOverWebSockets deals Euchre over real WebSockets and checks
// that no phone ever receives another player's cards, in any message.
func TestFourPhonesOverWebSockets(t *testing.T) {
	ts := newTestServer(t)
	clients := []*client{newClient(t, ts.URL)}
	clients[0].create("Ann", url.Values{"preset": {"euchre"}, "scoreMode": {"team"}})
	for _, n := range []string{"Bob", "Cy", "Dee"} {
		c := newClient(t, ts.URL)
		c.join(clients[0].code, n)
		clients = append(clients, c)
	}
	var phones []*ws
	for _, c := range clients {
		phones = append(phones, c.dial())
	}
	ann, dee := phones[0], phones[3]
	ann.ok("table.start", nil)
	for _, p := range phones {
		p.waitState(func(s map[string]any) bool { return s["table"] != nil })
	}
	ann.ok("pile.shuffle", map[string]any{"container": "deck"})
	ann.ok("cut.offer", map[string]any{"container": "deck", "to": dee.state["you"]})
	dee.waitState(func(s map[string]any) bool { return s["table"].(map[string]any)["cutOffer"] != nil })
	dee.ok("cut.answer", map[string]any{"count": 9})
	if r := step(ann, "deal", map[string]any{"pattern": "3-2"}); r["ok"] != true {
		t.Fatalf("deal: %v", r["error"])
	}

	hands := map[string][]string{}
	for _, p := range phones {
		me := p.state["you"].(string)
		s := p.waitState(func(s map[string]any) bool { return len(handIDs(s, "hand:"+me)) == 5 })
		hands[me] = handIDs(s, "hand:"+me)
	}
	// Bob plays a card face up: that one card becomes public.
	bob := phones[1]
	bobID := bob.state["you"].(string)
	played := hands[bobID][0]
	bob.ok("card.move", map[string]any{"ref": map[string]any{"container": "hand:" + bobID, "card": played}, "to": "trick", "face": "up"})
	for _, p := range phones {
		p.waitState(func(s map[string]any) bool { return len(handIDs(s, "trick")) == 1 })
	}

	for _, p := range phones {
		me := p.state["you"].(string)
		all := strings.Join(p.raw, "\n")
		for owner, ids := range hands {
			if owner == me {
				continue
			}
			for _, id := range ids {
				if id != played && strings.Contains(all, id) {
					t.Fatalf("%s received a card from %s's hand", me, owner)
				}
			}
		}
	}

	// A retried request is answered once and not applied twice.
	ann.n++
	dup := `{"id":"same","rev":0,"action":"turn.set","params":{"player":"` + ann.state["you"].(string) + `"}}`
	for range 2 {
		if err := ann.conn.Write(t.Context(), websocket.MessageText, []byte(dup)); err != nil {
			t.Fatal(err)
		}
	}
	var results []map[string]any
	for len(results) < 2 {
		if m := ann.read(); m["type"] == "result" && m["id"] == "same" {
			results = append(results, m)
		}
	}
	if results[0]["ok"] != true || results[1]["duplicate"] != true || results[0]["rev"] != results[1]["rev"] {
		t.Fatalf("duplicate handling: %v", results)
	}

	// Cy's phone drops and reconnects: same player, same five cards.
	cy := phones[2]
	cyID := cy.state["you"].(string)
	cy.conn.Close(websocket.StatusGoingAway, "")
	again := clients[2].dial()
	if again.state["you"] != cyID || strings.Join(handIDs(again.state, "hand:"+cyID), ",") != strings.Join(hands[cyID], ",") {
		t.Fatal("reconnect didn't restore Cy's hand")
	}
}
