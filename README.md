# Dibs

A virtual card table for phones. When friends have no deck, one person opens [dibs.mobasity.com](https://dibs.mobasity.com), creates a lobby, and everyone else joins from their own phone. They play the games they already know with real card handling: shuffle, offer a cut, deal in packets, draw, flip, peek, play tricks, keep score.

- **Games:** Euchre (guided from shuffle to 10 points), Texas Hold'em, six-card Golf, Blackjack, and a blank table for improvising anything else.
- **Private by construction:** the server holds the real deck and sends each phone only the cards its player may see. Hidden cards aren't hidden in the page; they never leave the server.
- **No accounts:** a name and a six-character code. Refreshing, locking the phone or losing signal keeps your seat and your cards.
- **Not a rules engine:** Dibs handles the cards; players handle the rules, the way they would at a real table. Nothing is automated unless a button says so.

**Documentation:** [DOCUMENTATION.md](DOCUMENTATION.md) covers the architecture, the card and privacy model, the WebSocket protocol, permissions, scores, presets and adding one, deployment and known limits. **Changes:** [CHANGELOG.md](CHANGELOG.md).

## Running it locally

Needs Go 1.27.3; Docker Desktop for the container run. Run the commands in Git Bash (or PowerShell, as noted) from the repository folder.

```bash
APP_ENV=dev go run ./cmd/dibs serve        # PowerShell: $env:APP_ENV="dev"; go run ./cmd/dibs serve
```

Success: the log shows `Dibs ready addr=[::]:2600`, and http://localhost:2600 shows the landing page with Create Lobby and Join Lobby. To play several players on one PC, give each its own browser profile or private window (each is a separate player), or open one on `http://localhost:2600` and another on `http://127.0.0.1:2600`. Phones on the same Wi-Fi can join at `http://<the PC's address>:2600` if Windows Firewall lets Go through: the server accepts any page served from the address the phone used.

In Docker, as Lighthouse runs it, published on `127.0.0.1:2601`:

```bash
docker network create spark                # once, if it doesn't exist yet
docker compose -f docker-compose.yml -f docker-compose.dev.yml up --build
```

Success: `docker ps` shows `dibs` as `healthy`, and http://localhost:2601 loads.

Nothing is written to disk: lobbies live in memory and end when the server stops.

## Tests

```bash
gofmt -l .                       # prints nothing
go vet ./...
go test -race ./...              # -race needs gcc (msys2 on Windows); plain go test ./... works without it
docker build --target test .     # exactly what Lighthouse runs before every deploy
go run golang.org/x/vuln/cmd/govulncheck@latest ./...   # "No vulnerabilities found."
```

The tests include a full Euchre game to 10 points through the real action API, four phones dealing over real WebSockets, privacy checks that search every player's actual messages for cards they shouldn't know, concurrent draws from one deck, retries, reconnects, and thousands of random or malformed requests against every action.

## Settings

All plain values, set in the compose file. Dibs has no secrets and nothing in Cove.

| Variable | Default | Meaning |
|---|---|---|
| `APP_ENV` | `prod` | `dev` adds debug logs, turns off static-file caching, and makes `http://localhost:<port>` the default public URL |
| `DIBS_ADDR` | `:2600` | Listen address |
| `DIBS_PUBLIC_URL` | `https://dibs.mobasity.com` | The address players open: share links, the cookie's `Secure` flag, HSTS and the WebSocket origin check use it |
| `DIBS_CLIENT_IP_HEADER` | empty | Header with the real client address behind a proxy (`CF-Connecting-IP` in prod), for rate limits. Only set it when the port can't be reached around the proxy |
| `DIBS_VERSION` | `dev-<commit>` | The version shown by `/health`; the compose file passes Lighthouse's deploy version |

## Commands

`dibs serve` runs the server (what the compose file starts). `dibs health` exits 0 when the server answers (the compose healthcheck; `docker exec dibs /dibs health`). `dibs version` and `dibs help`. Dibs has no operator shell: there's nothing to administer, and `/health` reports the number of open lobbies and connected players.
