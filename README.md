# Magnetor

Magnetor is a self-hosted cloud torrent client with a Go-powered web dashboard. Add magnet links or `.torrent` files, monitor transfers, and manage completed downloads from any browser. Run it locally or deploy it with Docker for a private, persistent torrent library.

> Use this software only to download and share content you are authorized to access. Follow applicable laws and provider terms.

## Features

- Single-owner local account with login/logout, profile and password change
- Add magnet links or upload `.torrent` files
- Live progress, peer count, and download/upload speeds
- Pause and resume active transfers
- Browse torrent files and download individual completed files, or a whole multi-file torrent as one ZIP
- Play completed video files in the browser when the container and codecs are supported
- Optional upload and seeding controls
- Persistent torrent library and settings across restarts
- Search Internet Archive and LibriVox, plus external torrent indexes
- Disk-space summary and configurable download directory
- `ctd` command to look up the username or reset a forgotten password

Search providers are external services; they can be unavailable or change their APIs and page layouts. Search results are not a guarantee of availability or permission to download.

## Project layout

```text
.
├── main.go, auth.go, ...      Go server (API, torrent engine, auth)
├── admin_cli.go               `ctd username` / `ctd password`
├── web/
│   ├── html/                  Pages (index, login, profile, settings)
│   ├── css/                   Styles
│   └── js/                    Browser scripts
├── Dockerfile                 Image build
├── docker-entrypoint.sh       Fixes /data ownership, then starts the app
├── docker-compose.yml         Compose service for the repository checkout
└── data/                      Created at runtime (ignored by Git)
    ├── downloads/             Downloaded content
    └── state/                 Account database, settings, torrent registry, engine state
```

## Where data is stored

Everything the app writes lives in one `data/` folder, so backing up or moving the app means copying that folder.

| Run mode | `data/` location |
| --- | --- |
| Local (`go run .`) | `./data` in the directory you start it from |
| Docker Compose | `./data` next to the `docker-compose.yml` you run |

Override the defaults with `DOWNLOAD_DIR` and `STATE_DIR` if needed.

## Install locally

Requirements: Go 1.24 or newer, Git, and a C compiler (`gcc`) because the torrent engine uses CGO.

- Debian/Ubuntu: `sudo apt install -y build-essential git`
- macOS: `xcode-select --install`
- Windows: use WSL2 (recommended) or install MSYS2/MinGW for `gcc`

```bash
git clone https://github.com/hekrun/magnetor.git
cd magnetor
go run .
```

Open [http://localhost:8080](http://localhost:8080) and create your account on first launch. Downloads go to `./data/downloads` and the account/state files to `./data/state`.

To use a different location:

```bash
DOWNLOAD_DIR=/path/to/downloads STATE_DIR=/path/to/state go run .
```

Run `go run .` from the repository root so the `web/` folder is found. To build a binary instead: `go build -o magnetor . && ./magnetor`.

### Run locally with systemd

On Linux servers using systemd, you can run the locally built Go server as a service and have it start automatically after reboot. Replace `YOUR_USER` below with the Linux account that owns the checkout (for example, the output of `whoami`), and update the paths if your checkout is elsewhere. Keep the checkout and its `web/` directory in place: the service uses it as its working directory.

Build the server and create its persistent data directories as that account:

```bash
cd /home/YOUR_USER/magnetor
go build -o magnetor .
mkdir -p /home/YOUR_USER/magnetor-data/downloads /home/YOUR_USER/magnetor-data/state
```

Create `/etc/systemd/system/magnetor.service`:

```ini
[Unit]
Description=Magnetor self-hosted torrent dashboard
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=YOUR_USER
Group=YOUR_USER
WorkingDirectory=/home/YOUR_USER/magnetor
Environment=DOWNLOAD_DIR=/home/YOUR_USER/magnetor-data/downloads
Environment=STATE_DIR=/home/YOUR_USER/magnetor-data/state
ExecStart=/home/YOUR_USER/magnetor/magnetor
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
```

Then load the unit and enable it now and on future boots:

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now magnetor
sudo systemctl status magnetor
```

The server listens on port `8080`. View logs with `sudo journalctl -u magnetor -f`. After building a new version, run `sudo systemctl restart magnetor`. To stop and disable automatic startup, run `sudo systemctl disable --now magnetor`.

## Install with Docker

Requirements: Docker Engine with the Compose plugin ([install guide](https://docs.docker.com/engine/install/)).

1. Create a folder for the app and move into it. The `data/` folder will be created here.

   ```bash
   mkdir magnetor && cd magnetor
   ```

2. Create `docker-compose.yml` in that folder:

   ```yaml
   services:
       magnetor:
          image: ghcr.io/hekrun/magnetor:latest
          container_name: magnetor
          restart: unless-stopped
          ports:
             - "3000:8080"
          environment:
             DOWNLOAD_DIR: /data/downloads
             STATE_DIR: /data/state
          volumes:
             # Relative to this file, so data/ is created next to the compose file you run.
             - ./data:/data
   ```

3. Start it:

   ```bash
   docker compose up -d
   ```

4. Open `http://localhost:3000` and create your account.

The `./data` path is relative to the compose file, so running Compose from any folder creates `data/downloads` and `data/state` in that same folder. On start the container fixes the ownership of `data/` automatically, so no `chmod`/`chown` is needed even though Docker creates the folder as root.

Change the left side of `3000:8080` to use another host port. To keep data elsewhere, replace `./data` with an absolute path such as `/srv/magnetor`.

To build from the repository instead of pulling the image, clone it and run `docker compose up -d --build` (the repository's `docker-compose.yml` has a `build:` section).

Useful commands (run in the compose folder):

```bash
docker compose pull          # Fetch the newest image
docker compose up -d         # Start or update the service
docker compose logs -f       # Follow logs
docker compose down          # Stop and remove the container; ./data is kept
```

The publish workflow builds `latest` on pushes to `main` and version tags (for example `v1.1.0`) on matching tag pushes. Pushes to `beta` do not trigger a build; to publish that branch, manually run the workflow with `beta` selected. Branch and version-tag images use their matching tags, for example `ghcr.io/hekrun/magnetor:beta`. If the package is private, run `docker login ghcr.io` first.

## Install on a cloud VPS

These steps assume a fresh Ubuntu/Debian VPS and SSH access.

1. Install Docker:

   ```bash
   curl -fsSL https://get.docker.com | sudo sh
   sudo usermod -aG docker $USER   # log out and back in afterwards
   ```

2. Create the app folder and compose file as described in [Install with Docker](#install-with-docker):

   ```bash
   mkdir -p ~/magnetor && cd ~/magnetor
   nano docker-compose.yml          # paste the YAML from above
   docker compose up -d
   ```

3. Open the firewall port (or the host port you chose):

   ```bash
   sudo ufw allow 3000/tcp
   ```

   Also allow it in your provider's cloud firewall/security group.

4. Visit `http://YOUR_VPS_IP:3000` and create the account right away. Registration closes once the first account exists, so do this before sharing the address.

5. Recommended: put the app behind HTTPS before using it over the internet. With a domain pointing at the VPS, a minimal [Caddy](https://caddyserver.com/) setup in `/etc/caddy/Caddyfile` is:

   ```text
   torrents.example.com {
       reverse_proxy localhost:3000
   }
   ```

   Then close direct access to port 3000 (`sudo ufw delete allow 3000/tcp`) and publish the port on localhost only by using `"127.0.0.1:3000:8080"` in the compose file.

To update later: `cd ~/magnetor && docker compose pull && docker compose up -d`.

## Account and password recovery

On first launch, create the single account with a username and password of at least 12 characters. Passwords are stored as bcrypt hashes; sessions expire after 30 days. There is no email or token recovery, so keep the password safe.

Use the **Profile** page to change the username or password (the current password is required).

If you forget them, use the `ctd` command on the server. The existing password cannot be shown because only its hash is stored; `ctd password` sets a new one and signs out all sessions.

```bash
# Docker (run in the compose folder)
docker compose exec magnetor ctd username
docker compose exec -it magnetor ctd password

# Local
go run . username
go run . password
```

## Settings

Open **Settings** in the dashboard (`/settings.html`) to change the download path, peer upload, and completed-torrent seeding. Restart the server after saving engine settings.

In Docker keep the download path inside `/data/downloads` so files stay in the mounted `data/` folder. Removing a torrent removes its downloaded files and prunes empty folders under the download directory.

## API

- `GET /api/torrents`: list torrents and file/progress details
- `POST /api/torrent`: add a magnet (`{"magnet":"magnet:..."}`)
- `POST /api/torrent-file`: upload a `.torrent` file as multipart field `file`
- `PATCH /api/torrent/{hash}`: pause or resume with `{"action":"stop"}` or `{"action":"start"}`
- `DELETE /api/torrent/{hash}`: remove a torrent and its downloaded files
- `GET /api/search?q=QUERY&provider=archive`: search a provider
- `GET /api/settings` and `PUT /api/settings`: read or save settings
- `GET /api/storage`: report disk capacity for the active download directory

All endpoints except health, auth status, login, registration, and logout require a signed-in session.

## Development checks

```bash
go test ./...
go vet ./...
for file in web/js/*.js; do node --check "$file"; done
docker compose config
```
