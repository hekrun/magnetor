# Magnetor Session Handoff

## Project

Magnetor is a self-hosted Go torrent dashboard using `anacrolix/torrent`, SQLite, and bcrypt. The web UI lives in `web/html`, `web/css`, and `web/js`; the server serves it on port 8080.

## Important Decisions

- There is one local owner account. First launch allows registration; later registration is closed.
- No email, SMTP, or token-based recovery. The `ctd username` and `ctd password` commands provide server-side recovery; password reset invalidates sessions.
- Keep downloads and state under the runtime `data/` directory (or configured `DOWNLOAD_DIR` / `STATE_DIR`). Never commit `data/`, databases, downloads, secrets, or raw chat transcripts.
- Docker Compose maps host port 3000 to container port 8080 and persists `/data`. The container runs the app as UID/GID 10001.

## Repository State

- Work is being prepared on branch `beta`.
- Tag `v1.1` already exists on the earlier beta release; do not move or overwrite it.
- The repo has been rebranded to Magnetor in the app, module path, Docker image references, and documentation. The GitHub repository rename is a manual step and may not yet have happened; check `git remote -v` before pushing.
- GitHub Actions currently publishes images on `main` and version-tag pushes, not on `beta` pushes.
- Codespace disk was full because of inactive Docker build cache. `docker builder prune -f` recovered about 2.7 GB; it removed build cache only, not app data.

## Verification

From the repository root:

```sh
go test ./...
go vet ./...
docker compose config
for file in web/js/*.js; do node --check "$file"; done
```

## New Codespace

Copilot chat history is not stored in this Git repository and may not follow a new Codespace. Open this file in the new Codespace, start a new Copilot chat, and ask it to read `SESSION_HANDOFF.md` before continuing. For exact conversation history, use the chat history/export feature if available and transfer only the relevant, secret-free parts separately.
