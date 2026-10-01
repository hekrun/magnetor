# Cloud Torrent

A self-hosted torrent manager with a Go download engine and a responsive dashboard. Add magnets or `.torrent` files, monitor transfers, and manage completed files from your browser.

> Use this software only to download and share content you are authorized to access. Follow applicable laws and provider terms.

## Features

- Single-owner local account with login/logout and SQLite-backed sessions
- Local single-owner account with password changes
- Add magnet links or upload `.torrent` files
- Live progress, peer count, and download/upload speeds
- Pause and resume active transfers
- Browse torrent files and download individual completed files
- Download all completed files in a multi-file torrent as one ZIP
- Play or open completed video files when your browser supports their container and codecs
- Optional upload and seeding controls
- Persistent torrent library and settings across restarts
- Search Internet Archive and LibriVox, plus external torrent indexes
- Disk-space summary and configurable download directory

Search providers are external services; they can be unavailable or change their APIs and page layouts. Search results are not a guarantee of availability or permission to download.

## Run Locally

Requirements: Go 1.24 or newer.

```bash
go run .
```

Open [http://localhost:8080](http://localhost:8080). By default, downloads go to `./downloads`.

To choose a download directory from the shell:

```bash
DOWNLOAD_DIR=/path/to/downloads go run .
```

The dashboard's **Settings** link opens `/settings.html`, where you can set the download path and configure peer uploads and seeding. Restart the server after saving engine settings for them to take effect.

## Deploy with Docker

The published image is hosted on GitHub Container Registry:

```text
ghcr.io/hekrun/cloud-torrent-downloader:latest
```

On a VPS with Docker Compose installed:

```bash
git clone https://github.com/hekrun/cloud-torrent-downloader.git
cd cloud-torrent-downloader
docker compose pull
docker compose up -d
docker compose logs -f
```

Open `http://YOUR_VPS_IP:3000`. The app listens directly on host port `3000` in Docker Compose. The repository's GitHub Actions workflow publishes images to GHCR on pushes to `main` and version tags. If the package is private, authenticate with `docker login ghcr.io` before pulling.

Compose bind-mounts `./data` from the repository directory to `/data` in the container, so all runtime data survives container replacement:

- `/data/downloads`: downloaded content
- `/data/state`: settings, torrent registry, uploaded metadata, and engine state

Useful commands:

```bash
docker compose pull                 # Fetch the newest published image
docker compose up -d                 # Start or update the service
docker compose logs -f               # Follow logs
docker compose down                  # Stop the service; keep the data volume
```

To use another host directory, edit the left side of the `/data` volume mapping in `docker-compose.yml`. `docker compose down` does not delete the host data directory.

## Settings and Storage

Use the **Settings** link or open `http://localhost:3000/settings.html` to configure the server download path, peer uploads, and completed-torrent seeding. Engine settings apply after restarting the server.

- Local settings page: `http://localhost:8080/settings.html`
- VPS settings page: `http://YOUR_VPS_IP:3000/settings.html`
- Default local download directory: `./downloads`
- Docker download directory: `/data/downloads`
- Runtime state is stored separately from downloaded content
- Removing a torrent removes its downloaded files and prunes empty folders under the download directory

## Login and Security

On first launch, create the single local account with a username and password of at least 12 characters. Registration closes once the account is created. Passwords are stored as bcrypt hashes; sessions are stored in `accounts.sqlite` under the state directory and expire after 30 days. There is no setup token or password-recovery flow, so restrict access until the first account exists and keep the password safe.

Use the **Profile** page to update your username or change your password. Password changes require the current password.

If the username is forgotten, run `docker compose exec cloud-torrent ctd username`. To replace a forgotten password, run `docker compose exec -it cloud-torrent ctd password`; enter the new password twice at the hidden prompts. The existing password cannot be displayed because only its bcrypt hash is stored. Password reset signs out all active sessions. For local runs, use `go run . username` or `go run . password`.

For a VPS, put the app behind HTTPS before entering account credentials over the internet. Restrict access to the app's port until the first account has been created.

## API

- `GET /api/torrents`: list torrents and file/progress details
- `POST /api/torrent`: add a magnet (`{"magnet":"magnet:..."}`)
- `POST /api/torrent-file`: upload a `.torrent` file as multipart field `file`
- `PATCH /api/torrent/{hash}`: pause or resume with `{"action":"stop"}` or `{"action":"start"}`
- `DELETE /api/torrent/{hash}`: remove a torrent and its downloaded files
- `GET /api/search?q=QUERY&provider=archive`: search a provider
- `GET /api/settings` and `PUT /api/settings`: read or save settings
- `GET /api/storage`: report disk capacity for the active download directory

Runtime settings, torrent registry, uploaded metadata, and engine databases are stored under `/data/state` in Docker. Downloads are stored under `/data/downloads`.

## Development Checks

```bash
go test ./...
node --check web/app.js
node --check web/settings.js
docker compose config
```
