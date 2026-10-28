# Dibs: documentation

How Dibs works inside, for whoever changes it next. The README covers running it.

## Contents

1. [Overview](#1-overview)
2. [Deployment](#2-deployment)
3. [Known limits](#3-known-limits)

## 1. Overview

One Go binary (`cmd/dibs`) serves server-rendered pages, static files and one WebSocket per player. All state is in memory: **a restart or deploy ends every lobby.** That's a deliberate choice for v0: a game at a bar lasts an evening, and nothing worth keeping is lost.

| Package | Does |
|---|---|
| `internal/config` | Reads the settings from the environment |
| `internal/random` | Unpredictable codes, tokens and IDs from `crypto/rand` |
| `internal/lobby` | Lobbies, players, sessions, seats, teams, owner controls; the per-lobby lock every action goes through; per-player snapshots |
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

## 5. Known limits

- State is in memory only: a restart ends every lobby.
