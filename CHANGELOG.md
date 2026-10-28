# Changelog

All notable changes to Dibs. Versions follow [semantic versioning](https://semver.org).

## v0.0.0 (2026-10-28)

The first version: a multiplayer card table for phones, at dibs.mobasity.com.

### Added
- A plain landing page with Create Lobby and Join Lobby.
- A health endpoint, `/health`, reporting the running version.
- Docker image and compose file for Lighthouse: tests run before every deploy, the container runs read-only as a non-root user, and the site is reached only through Cloudflare.
