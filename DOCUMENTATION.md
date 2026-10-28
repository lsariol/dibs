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
8. [Known limits](#8-known-limits)

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
- **Flooding:** each connection gets 10 actions a second (bursts of 40); beyond that, actions are refused with a message.
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

## 8. Known limits

- State is in memory only: a restart ends every lobby.
