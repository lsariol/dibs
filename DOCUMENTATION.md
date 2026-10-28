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
| `internal/server` | HTTP: pages, static files, health, security headers, graceful shutdown |
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

## 3. Known limits

- State is in memory only: a restart ends every lobby.
