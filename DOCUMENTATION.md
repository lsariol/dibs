# Dibs: documentation

How Dibs works inside, for whoever changes it next. The README covers running it.

## Contents

1. [Overview](#1-overview)
2. [Deployment](#2-deployment)
3. [Lobbies and sessions](#3-lobbies-and-sessions)
4. [Realtime protocol](#4-realtime-protocol)
5. [Cards and the table](#5-cards-and-the-table)
6. [Hidden information](#6-hidden-information)
7. [Scores](#7-scores)
8. [Game presets](#8-game-presets)
9. [Known limits](#9-known-limits)

## 1. Overview

One Go binary (`cmd/dibs`) serves server-rendered pages, static files and one WebSocket per player. All state is in memory: **a restart or deploy ends every lobby.** That's a deliberate choice for v0: a game at a bar lasts an evening, and nothing worth keeping is lost.

| Package | Does |
|---|---|
| `internal/config` | Reads the settings from the environment |
| `internal/random` | Unpredictable codes, tokens and IDs from `crypto/rand` |
| `internal/lobby` | Lobbies, players, sessions, seats, teams, owner controls; the per-lobby lock every action goes through; per-player snapshots |
| `internal/cards` | Card faces (definitions) and decks of unique card instances; shuffle and cut. No tables or games |
| `internal/table` | The generic table: containers, card state, every physical action, permissions, visibility, per-player views |
| `internal/presets` | Game setups built only from table actions |
| `internal/server` | HTTP: pages, forms, the WebSocket, rate limits, security headers, graceful shutdown |
| `web` | Templates, CSS and JavaScript, embedded in the binary |

## 2. Deployment

Lighthouse deploys Dibs from its repository; nothing is set up on the server for it besides the Cloudflare route.

- **Compose project `dibs`**, one service `dibs` (container `dibs`) on the `spark` network, listening on 2600. The port is exposed, not published: the only way in is cloudflared, routing `dibs.mobasity.com` to `http://dibs:2600`.
- **`x-lighthouse: {deploy: releases}`**: only `v*` tags on `main` deploy.
- **Tests before every deploy:** the Dockerfile's `test` stage runs gofmt, vet and `go test -race`.
- **Healthcheck:** `/dibs health` asks `http://127.0.0.1:2600/health` inside the container.
- **Hardening:** read-only filesystem, user 10001, all capabilities dropped, `no-new-privileges`, 256 MB memory limit. Dibs writes nothing to disk.
- **No secrets, no database, no volumes.** Nothing in Cove.
- **Label** `com.mobasity.public-url: https://dibs.mobasity.com`, written as a map (Lighthouse doesn't read list-form labels), so Sonar checks it from outside.
- **WebSockets through Cloudflare** need nothing special. Dibs pings every connection well inside Cloudflare's 100-second idle timeout.
- **Graceful shutdown:** on SIGTERM the server stops accepting connections and closes open WebSockets (their requests' context ends); in-memory lobbies end with it.
- **Headers:** a strict Content Security Policy (scripts, styles and connections from the same origin only, no inline code, no framing), `nosniff`, `Referrer-Policy: same-origin`, and HSTS when the public URL is https.
- **Static files** are served under `/static/<version>/…` and cached for a year; each release changes the path.

**Before the first deploy** (Lu's steps, in `prod-rollout-plan.md` Appendix B): the GitHub repository, the `lighthouse-publisher` App and rulesets, `lh add` **before** any tag exists (so `v0.0.0` is deployed and published, not taken as a baseline), the Cloudflare public hostname `dibs.mobasity.com` → `http://dibs:2600`, then the `v0.0.0` tag.

## 3. Lobbies and sessions

- **Codes** are 6 characters from a 31-letter alphabet without look-alikes (no 0/O, 1/I/L): about 887 million codes. Typed codes ignore case, spaces and dashes.
- **Guessing** is rate limited per client address: 20 join attempts and 10 unknown codes a minute. Once the unknown-code budget is spent, every code (real ones too) answers "no open lobby" until it refills, so a guesser learns nothing. Creating lobbies is limited to 6 a minute. Behind Cloudflare the address comes from `CF-Connecting-IP` (`DIBS_CLIENT_IP_HEADER`); the port isn't reachable any other way.
- **Sessions:** joining sets a cookie `dibs_session` with 32 random bytes, `HttpOnly`, `SameSite=Lax`, `Secure` on https, scoped to `/l/<code>` (one per lobby, so one browser can be in several). The server keeps only its SHA-256. Share links never carry it. Removing a player or ending a lobby deletes their sessions.
- **Forms** are protected against cross-site posts by Go's `http.CrossOriginProtection` (Sec-Fetch-Site and Origin checks). **WebSockets** accept only the public URL's origin (or the same host) and need the session cookie.
- **Disconnects are never leaves.** A closed connection only marks the player as disconnected; their seat, cards and score stay. Leaving is an explicit action. Reconnecting with the same cookie restores the same player and sends a fresh snapshot.
- **Ownership:** the creator owns the lobby. If the owner leaves, the longest-present player takes over; the owner can also hand it over. Ownership never shows anyone else's cards.
- **Expiry:** a lobby closes after 2 hours with nobody connected, or 6 hours with no actions or connection changes at all. Players still connected are told why.

## 4. Realtime protocol

One WebSocket per open tab at `/l/<code>/ws`. Messages are JSON text frames, at most 16 KB from the client.

**Client → server:** `{"id": "<unique per player>", "rev": <last revision seen>, "action": "<name>", "params": {...}}`.

**Server → client:**

| Message | When |
|---|---|
| `{"type": "state", "rev": N, "you": "<player id>", "lobby": {...}, "log": [...], ...}` | After every change, and on connect. Always the complete view for that player; there are no diffs, so a client that missed messages is correct after the next one |
| `{"type": "result", "id": "...", "ok": true, "rev": N}` or with `"error": {"code", "message"}` | The answer to one request |
| `{"type": "ended", "reason": "..."}` | The lobby ended, or the player was removed; the server then closes the socket |

**Guarantees:**
- Each lobby has one lock. An action is authorized, validated and applied entirely under it, then the revision goes up by one and every connection gets its new snapshot. A failed action changes nothing (not even the history).
- **Duplicates:** the server remembers each player's last 128 request IDs and their results. A retry (the client resends anything unanswered after a reconnect) returns the first result with `"duplicate": true` instead of acting twice.
- **Slow phones:** a connection holds only the newest unsent snapshot, so a slow client skips stale ones instead of queueing them. Results queue up to 32; a client that can't keep up is disconnected and catches up on reconnect.
- **Flooding:** each connection gets 10 actions a second (bursts of 40); beyond that, actions are refused with a message. A player can have at most 5 tabs connected; a sixth is told to close one.
- Pings every 25 seconds keep the connection alive through Cloudflare.
- Error codes: `bad_request` (malformed), `forbidden` (not allowed for you), `not_found`, `invalid` (not valid right now), `stale` (based on an old view; the client should retry with the new state), `full`.

**Lobby actions:** `lobby.seat {player?, seat}`, `lobby.team {player, team}`, `lobby.ready {ready}`, `lobby.rename {name}`, `lobby.kick {player}`, `lobby.leave`, `lobby.owner {player}`, `lobby.settings {...}`, `lobby.end`, `state.sync` (no change; returns the current revision).

## 5. Cards and the table

**Definitions and instances.** A `cards.Def` is a face (rank, suit, color). A `cards.Card` is one physical card: a random ID, a face, and which deck it came from, so two decks' Aces of Spades are different cards. Deck types: `standard52`, `standard54`, `euchre24`, `color108` (a generic 108-card color/number/action deck), `custom` (chosen ranks and suits, 0-4 jokers), each 1-8 times.

**Where, who, and who sees** are separate:
- *Location:* every card is in exactly one container. `Stack` is an ordered pile (the last card is the top); `Hand` is a player's private hand; `Grid` is fixed slots (Golf's 2x3, the Hold'em board).
- *Authority:* a container with an `Owner` can be taken from, managed and peeked into only by that owner. Anyone seated can *put* cards into it (giving, dealing). Shared containers have four access levels, `Take`, `Place`, `Manage` (shuffle, cut, deal, collect) and `Peek`, each `anyone`, `dealer` or `nobody`. The lobby's *shared* policy turns `dealer` into `anyone`, and never touches personal containers. Being the lobby owner grants nothing here, except gathering every card back at the end of a hand and naming the dealer.
- *Visibility:* a card is visible to a player if it's face up, it's in their own hand, or they peeked at it. Peeks are forgotten when the card leaves its container or its pile is shuffled or cut. Every peek is announced in the history.

**Atomic actions.** The lobby runs every table action on a deep copy of the table. If the action fails, or the copy fails `Check()` (every card in exactly one slot of the container it records), the copy is thrown away and nothing changed. Multi-card actions (a whole deal, gathering everything) are therefore all-or-nothing. A step-by-step deal is a small state machine (`PendingDeal`): each packet is its own atomic action, and nothing else can deal from, shuffle or cut that pile meanwhile.

**Referring to cards.** A client names a card by ID when it can see it, and by position otherwise (`index`, or `-1` for the top). A position in an ordered container is refused as `stale` if the container changed after the revision the client sent. The top card and grid slots never go stale. Hidden IDs are never sent, so a client can't name a card it hasn't seen.

**Cuts.** `Cut(k)` lifts the top `k` cards and puts them underneath (a rotation, not a shuffle). An offered cut (`cut.offer`) blocks the pile until the chosen player cuts (`cut.answer {count}`) or declines (`count: 0`), or the dealer withdraws the offer.

**Deals.** `deal.start` takes the source pile, the recipients in dealing order (hands, grids or shared piles), and `passes`: per pass, either one packet size for everyone or one per recipient (`[[2,3,2,3],[3,2,3,2]]`). Face up/down, and all at once or `step` (then `deal.step {all}` / `deal.cancel`). Grids fill their first empty slots.

**Undo.** Only the last action on the lobby, only by the player who did it, and only if no card became visible to anyone new (compared before and after for every card and player). A draw, a face-up play or a peek can't be undone: the information is out. Fix those forward with an ordinary move. Shuffles and cuts are never undoable.

**Table actions:** `table.start`, `table.stop`, `table.reset`, `card.move {ref, to, index?, face?, displaceTo?}`, `card.flip {ref}`, `card.peek {ref}`, `card.draw {from, to?, count, face?}`, `card.blind_draw {from, index}` (Old Maid; only when the lobby allows it), `hand.reveal {up}`, `hand.sort {by}`, `pile.shuffle`, `pile.cut {container, count}`, `pile.collect {from, to}`, `pile.collect_all {to}`, `pile.create`, `pile.remove`, `cut.offer {container, to}`, `cut.answer {count}`, `cut.cancel`, `deal.start`, `deal.step {all}`, `deal.cancel`, `dealer.set {player}`, `dealer.pass`, `round.next {to}` (gather everything into `to`, then pass the deal; dealer or owner), `turn.set {player}`, `turn.advance {dir}`, `action.undo`.

## 6. Hidden information

Hidden cards aren't hidden with CSS: they aren't sent. `Table.ViewFor(player)` builds each player's view:
- A card the viewer may not know is sent as `{"i": <position>}`: no ID, no face. IDs are random and only sent for cards the viewer can see, so a hidden card can't be followed from one place to another.
- A closed pile (deck, discard, kitty) sends its count and its top card only. Grids, hands and spread piles send every slot.
- A peeked card shows its face to the peeker only (`mine: true`), and nothing to anyone else.
- The history is written by the server and only names cards that are face up at that moment; it never holds a hidden identity, so it needs no filtering.
- Nothing else carries card data: pages are server-rendered without game state, the browser stores nothing but the HttpOnly session cookie, and errors never name hidden cards.

`internal/lobby/game_test.go` checks each player's actual JSON snapshot for every hidden card's ID and face, after shuffles, deals, plays, blind draws and refused actions, and fails if any appear.

## 7. Scores

`internal/scoring` is a generic scoreboard; the lobby keeps one per lobby, so scores survive a fresh table and going back to the lobby.

- **Entries** are rebuilt from the seated players when the table starts, keeping existing scores: per player in *solo*, per team in *team*, and in *owner* per team when anyone is on a team, else per player.
- **Who may change an entry** (checked by the server on every change, whatever the page shows):
  - `solo`: each player, only their own entry, even in a team game (the page says so). The owner gets no exception.
  - `team`: only the team's appointed scorekeeper, only that team's entry. When the table starts, each team without a valid keeper gets its first seated member; the owner can appoint another member (`score.keeper`). Owning the lobby doesn't bypass this.
  - `owner`: only the lobby owner.
- **History and corrections:** every change is recorded with who, when, how much and why. `score.revert {change}` adds an opposite change marked as a correction; whoever may edit that entry may revert it, once. `score.reset` (owner) zeroes the scores and clears the history.
- **Preset scoring** (Euchre's hand results) goes through the same function as a manual change, so the same permissions apply.
- **Target:** reaching it is shown to everyone; nothing ends automatically.

**Score actions:** `score.add {entry, delta, reason?}`, `score.revert {change}`, `score.reset`, `score.keeper {team, player}`, `score.target {target}`. Entry IDs are player IDs or `team:<A-D>`.

## 8. Game presets

A preset (`internal/presets`) sets up a table for a game and, optionally, guides the players through it. It has no card engine of its own: setup adds containers and cards through the table API, and every guided step is either an ordinary table action or a preset step (`preset.step {step, args}`) that calls the same table functions, with the same permission checks. Nothing is automatic: each step is a button someone presses, the generic actions stay available for house rules, and turns aren't enforced. Each snapshot carries the viewer's `guide`: a status line, an optional hint, and the buttons that apply to them now.

| Preset | Setup | Guided steps |
|---|---|---|
| **Blank table** (2-8) | The lobby's deck (any type, 1-8 decks), Discard, an open Table area, hands, optional personal grids | None; anything goes |
| **Euchre** (4) | 24-card deck, a spread Trick pile, a won-tricks pile per team, hands; partners opposite (the lobby pairs seats 1&3 and 2&4 if teams aren't already set that way); team scoring to 10 | Shuffle; offer the cut to the dealer's right; deal clockwise from the dealer's left in two passes (3-2/2-3, 2-3/3-2, 2s then 3s, 3s then 2s; all at once or packet by packet); turn up the top of the 4-card kitty; round 1 pass / order up / pick up, alone or not; dealer picks up and discards (an ordinary move, so cards are never teleported); turn it down; round 2 call any other suit; throw in the hand if all pass; award each trick to the player who won it (counted per team); score the hand (3-4 tricks 1 point, march 2, lone march 4, euchred 2 to the defenders) through the scoreboard's permissions; next hand (gather, pass the deal left) |
| **Texas Hold'em** (2-8) | 52-card deck, a 5-slot Board, a Burn pile nobody can look at or take from, a Folded pile, hands; solo scores for chip counts | Shuffle, offer the cut; deal two hole cards one at a time from the dealer's left; burn and flop / turn / river; show my cards; fold; next hand (the button moves left). The status line names the button and blinds (heads-up, the button is the small blind). No betting, pot or hand ranking |
| **Six-card Golf** (2-8) | 52-card deck (two past four players), face-up Discard, a 2x3 grid and a hand per player; solo scores | Deal six to each grid face down (unknown even to their owner); turn up the first discard; turn all my cards up; record my score by the common count (A 1, 2 −2, J/Q 10, K 0, a matching column 0) as a suggestion, or enter scores by hand; next hole. Flips, draws and swaps are ordinary actions |
| **Blackjack** (2-8) | A shoe of the lobby's deck count, the Dealer's hand (spread, hole card face down, only the dealer may peek), a Discard, a face-up spot per player; solo scores | Deal the round (one up to each player and the dealer, then again with the dealer's face down); hit me / stand (players, own spot only, not after standing); hit a player or the dealer, peek at and turn over the hole card (dealer); collect; shuffle the discards back into the shoe. No bets or payouts |

**Adding a preset:** add a file in `internal/presets` with a type implementing `Preset` (`Info`, `Setup`) and, for guidance, `Guided` (`Guide`, `Do`); call `register` in its `init` and add its ID to `order`. Build the table only with `table` functions; use `Transfer` only for a procedure the preset has already authorized (a Blackjack hit from the dealer's shoe). Score through `env.AddScore`, never the board directly. Add a lobby test that plays it through `Lobby.Do`, with `noLeaks` after anything that moves cards.

## 9. Known limits

- **Memory only.** A restart or deploy ends every lobby in progress (players see "Session over"). Deploy when nobody is playing. Persistence would go behind the `Lobby`/`Manager` boundary.
- **One instance.** Lobbies live in one process; Dibs can't be scaled out without moving state to a shared store.
- **No rules enforcement.** Turns are only an indicator; Euchre's follow-suit, trump ranking and trick winners are the players' call (they pick who won each trick); Hold'em has no betting or hand ranking; Blackjack has no bets, payouts or house rules. These belong in a later automated mode built on the same table actions.
- **Undo** takes back only the latest action in the lobby, by the player who did it, and only if it revealed nothing. Everything else is fixed forward (move the card back; score corrections are reverts).
- **Seats are fixed while the table runs.** Someone who joins mid-game watches (public cards only) until the owner returns everyone to the lobby and starts again; scores carry over.
- **One seat per browser per lobby.** The session is a cookie; clearing it, or switching browsers, means joining again as a new player (the owner can remove the old one). Sessions last 24 hours.
- **Hidden cards can be named by position.** A player can refer to an unseen card only by its position (as you'd point at a card back); positions in a pile that changed since the player's last view are refused as stale.
- **Taps only.** Every action is a tap and a choice; there's no drag and drop, animation or sound.
- **Not tested:** screen readers (cards and buttons have labels, but nobody has played with one), and real phones on mobile networks behind Cloudflare. Browser tests ran in Chromium at 320 and 390 px wide, light and dark.
