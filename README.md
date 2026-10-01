# Cloud Torrent

A self-hosted torrent manager with a Go download engine and a responsive web dashboard. Add magnets or `.torrent` files, monitor transfers, and manage completed downloads from your browser.

> Use this software only to download and share content you are authorized to access. Follow applicable laws and provider terms.

## Features

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

Open `http://YOUR_VPS_IP:8080`. If the GHCR package is private, authenticate on the VPS with `docker login ghcr.io` first. The repository's GitHub Actions workflow publishes images on pushes to `main` and version tags.

Compose keeps persistent data in the `cloud-torrent-data` volume:

- `/data/downloads`: downloaded content
- `/data/state`: settings, torrent registry, uploaded metadata, and engine state

Useful commands:

```bash
docker compose pull                 # Fetch the newest published image
docker compose up -d                 # Start or update the service
docker compose logs -f               # Follow logs
docker compose down                  # Stop the service; keep the data volume
```

Do not run `docker compose down -v` unless you intend to delete the persistent volume and its contents.

## Settings and Storage

- Settings page: `http://localhost:8080/settings.html`
- Default local download directory: `./downloads`
- Docker download directory: `/data/downloads`
- Runtime state is stored separately from downloaded content
- Removing a torrent removes its downloaded files and prunes empty folders under the download directory

## API

- `GET /api/torrents`: list torrents and file/progress details
- `POST /api/torrent`: add a magnet (`{"magnet":"magnet:..."}`)
- `POST /api/torrent-file`: upload a `.torrent` file as multipart field `file`
- `PATCH /api/torrent/{hash}`: pause or resume with `{"action":"stop"}` or `{"action":"start"}`
- `DELETE /api/torrent/{hash}`: remove a torrent and its downloaded files
- `GET /api/search?q=QUERY&provider=archive`: search a provider
- `GET /api/settings` and `PUT /api/settings`: read or save settings
- `GET /api/storage`: report disk capacity for the active download directory

## Development Checks

```bash
go test ./...
node --check web/app.js
node --check web/settings.js
docker compose config
```