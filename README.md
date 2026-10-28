# Dibs

A virtual card table for phones. When friends have no deck, one person opens [dibs.mobasity.com](https://dibs.mobasity.com), creates a lobby, and everyone else joins from their own phone. They play the games they already know (Euchre, Hold'em, Golf, Blackjack, or anything improvised) with real card handling: shuffle, offer a cut, deal in packets, draw, flip, peek, play tricks, keep score.

The server holds the real deck. Each phone only ever receives the cards its player is allowed to see.

- **Documentation:** [DOCUMENTATION.md](DOCUMENTATION.md): architecture, the card and privacy model, the WebSocket protocol, permissions, presets, limits.
- **Changes:** [CHANGELOG.md](CHANGELOG.md).

## Running it locally

Needs Go 1.27.3; Docker Desktop for the container run. Run the commands in PowerShell or Git Bash from the repository folder.

```bash
APP_ENV=dev go run ./cmd/dibs serve          # Git Bash; in PowerShell: $env:APP_ENV="dev"; go run ./cmd/dibs serve
```

Success: the log shows `Dibs ready addr=[::]:2600`, and http://localhost:2600 shows the landing page. To try multiplayer on one PC, open the lobby in a normal window and a private window (each is its own player). Phones on the same Wi-Fi can't reach it this way: the dev server only trusts `localhost` as an origin.

In Docker, as Lighthouse runs it, with the port published on `127.0.0.1:2601`:

```bash
docker network create spark                  # once, if it doesn't exist yet
docker compose -f docker-compose.yml -f docker-compose.dev.yml up --build
```

Success: `docker ps` shows `dibs` as `healthy`, and http://localhost:2601 loads.

A local run writes nothing to disk: lobbies live in memory.

## Tests

```bash
gofmt -l .                       # prints nothing
go vet ./...
go test ./...                    # add -race when gcc is installed (msys2 on Windows)
docker build --target test .     # exactly what Lighthouse runs before a deploy
```

## Settings

All plain values, set in the compose file. Dibs has no secrets.

| Variable | Default | Meaning |
|---|---|---|
| `APP_ENV` | `prod` | `dev` adds debug logs and makes `http://localhost:<port>` the default public URL |
| `DIBS_ADDR` | `:2600` | Listen address |
| `DIBS_PUBLIC_URL` | `https://dibs.mobasity.com` | The address players open: share links, the cookie's `Secure` flag and the WebSocket origin check use it |
| `DIBS_CLIENT_IP_HEADER` | empty | Header with the real client address behind a proxy (`CF-Connecting-IP` in prod), for rate limits. Only set it when the port can't be reached around the proxy |
| `DIBS_VERSION` | `dev` | The version shown by `/health`; the compose file passes Lighthouse's deploy version |

## Commands

`dibs serve` runs the server; `dibs health` exits 0 when it answers (the compose healthcheck); `dibs version`; `dibs help`.
