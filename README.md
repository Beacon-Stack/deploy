# Beacon Stack — Deploy

Docker Compose deployment for the Beacon media management stack.

[Website](https://beaconstack.io) | [Pulse](https://github.com/beacon-stack/pulse) | [Pilot](https://github.com/beacon-stack/pilot) | [Prism](https://github.com/beacon-stack/prism) | [Haul](https://github.com/beacon-stack/haul)

---

This repo contains a single `docker-compose.yml` that wires up the full Beacon stack: Postgres, Pulse (control plane), Pilot (TV), Prism (movies), Haul (BitTorrent), and Gluetun (VPN tunnel). Clone it, edit two files, run one command.

```
                    ┌─────────────┐
                    │   Postgres  │
                    │   :5432     │
                    └──────┬──────┘
                           │
                    ┌──────▼──────┐
                    │    Pulse    │
                    │   :9696     │
                    │ control     │
                    │   plane     │
                    └──┬──────┬───┘
             registers │      │ registers
                ┌──────▼──┐ ┌─▼───────┐
                │  Pilot  │ │  Prism  │
                │  :8383  │ │  :8282  │
                │   (TV)  │ │(movies) │
                └────┬────┘ └────┬────┘
                     │           │
                     └─────┬─────┘
                      grab │ torrent
                  ┌────────▼────────┐
                  │ ╔══════════════╗│
                  │ ║  VPN tunnel  ║│
                  │ ║  (Gluetun)   ║│
                  │ ╠══════════════╣│
                  │ ║    Haul      ║│
                  │ ║    :8484     ║│
                  │ ╚══════════════╝│
                  └─────────────────┘
```

## Prerequisites

- Docker Engine 24+ and Docker Compose v2.20+
- A VPN subscription (recommended for Haul; [can be disabled](#disabling-vpn))
- At least 2 GB of available RAM

## Quick start

```bash
# 1. Clone this repo
git clone https://github.com/beacon-stack/deploy.git
cd deploy

# 2. Copy and edit the environment file
cp .env.example .env
# Open .env and set your VPN provider, server region, and media paths.

# 3. Copy and edit the secret files
cp secrets/pg-password.txt.example secrets/pg-password.txt
cp secrets/vpn-username.txt.example secrets/vpn-username.txt
cp secrets/vpn-password.txt.example secrets/vpn-password.txt
# Replace the placeholder text in each file with your real values.

# 4. Start the stack
docker compose up -d

# 5. Check that everything is healthy
docker compose ps
```

Once all services show `healthy`, open the web UIs:

| Service | URL | Purpose |
|---|---|---|
| Pulse | http://localhost:9696 | Control plane — manage shared indexers, quality profiles, and settings |
| Pilot | http://localhost:8383 | TV series management — add shows, monitor episodes, grab releases |
| Prism | http://localhost:8282 | Movie collection — add movies, edition-aware scoring, Radarr v3 API |
| Haul | http://localhost:8484 | BitTorrent client — downloads, stall detection, VPN-aware dashboard |

Each app generates an API key on first run. Find it in the app's Settings page.

## Connecting the apps

After first startup:

1. **Add Haul as a download client in Pilot and Prism.** In each app's Settings, add a download client with URL `http://vpn:8484` and the API key from Haul's settings page. The URL is `vpn` (not `haul`) because Haul runs inside the VPN container's network namespace.

2. **Indexers and quality profiles are shared automatically.** If Pulse is running, Pilot and Prism registered with it on startup. Add indexers in Pulse's web UI and they flow to every subscribed service.

## Configuration

### Environment variables

All configurable values live in `.env`. The file is organized by service with comments explaining each variable. See `.env.example` for the full reference.

### Docker secrets

Passwords are stored in `secrets/*.txt` files, not in `.env`. Docker mounts these files into containers at `/run/secrets/` and they never appear in `docker inspect` output or the process environment.

| File | What goes in it |
|---|---|
| `secrets/pg-password.txt` | Postgres superuser password |
| `secrets/vpn-username.txt` | VPN username (OpenVPN) |
| `secrets/vpn-password.txt` | VPN password (OpenVPN) |

Each file must contain only the secret value with no trailing newline. Use `echo -n "mypassword" > secrets/pg-password.txt` or a text editor that doesn't append newlines.

### Database passwords

The per-app database passwords (pulse/pilot/prism/haul) are hardcoded in both `docker-compose.yml` (in the `DATABASE_DSN` environment variables) and `init-db.sql` (in the `CREATE USER` statements). The defaults are the app name as the password (e.g., `pulse`/`pulse`).

If you want to change them, edit **both files** before the first `docker compose up`. After first run, Postgres has already initialized the users — changing passwords requires dropping the volume and re-initializing:

```bash
docker compose down -v   # deletes pgdata — all data is lost
# Edit init-db.sql and docker-compose.yml with new passwords
docker compose up -d
```

### Media paths

The default paths are relative to the deploy directory (`./data/downloads`, `./data/tv`, `./data/movies`). This makes the stack self-contained. Power users can override them with absolute paths in `.env` for NAS mounts or existing media directories:

```env
DOWNLOADS_PATH=/mnt/nas/downloads
TV_PATH=/mnt/nas/media/tv
MOVIES_PATH=/mnt/nas/media/movies
```

Pilot, Prism, and Haul all mount `DOWNLOADS_PATH` so they can see completed downloads and import/hardlink them into the media directories.

## VPN configuration

The stack uses [Gluetun](https://github.com/qdm12/gluetun), which supports 30+ VPN providers. The default configuration is PIA (Private Internet Access) over OpenVPN.

### Switching providers

Set `VPN_SERVICE_PROVIDER` and the required auth variables in `.env`. Each provider has different requirements — see the [Gluetun wiki](https://github.com/qdm12/gluetun-wiki/tree/main/setup/providers) for your provider's page.

**PIA** (default):
```env
VPN_SERVICE_PROVIDER=private internet access
VPN_TYPE=openvpn
VPN_SERVER_REGIONS=Netherlands
VPN_PORT_FORWARDING=on
```

**Mullvad** (WireGuard):
```env
VPN_SERVICE_PROVIDER=mullvad
VPN_TYPE=wireguard
WIREGUARD_PRIVATE_KEY=your-key-here
WIREGUARD_ADDRESSES=10.x.x.x/32
VPN_SERVER_REGIONS=Netherlands
```

**NordVPN**:
```env
VPN_SERVICE_PROVIDER=nordvpn
VPN_TYPE=openvpn
VPN_SERVER_REGIONS=Netherlands
```

**ProtonVPN**:
```env
VPN_SERVICE_PROVIDER=protonvpn
VPN_TYPE=openvpn
VPN_SERVER_REGIONS=Netherlands
VPN_PORT_FORWARDING=on
```

**Surfshark**:
```env
VPN_SERVICE_PROVIDER=surfshark
VPN_TYPE=openvpn
VPN_SERVER_REGIONS=Netherlands
```

For WireGuard providers, uncomment the `WIREGUARD_*` lines in `docker-compose.yml` under the vpn service and set them in `.env`. The OpenVPN secret files can be left with placeholder values — Gluetun ignores them when using WireGuard.

### Port forwarding

VPN port forwarding allows incoming torrent connections, which improves download speeds and peer availability. PIA and ProtonVPN support it natively through Gluetun. Set `VPN_PORT_FORWARDING=on` in `.env`.

### Disabling VPN

If you don't need a VPN for torrent traffic, follow the instructions in the comment block above the `vpn` service in `docker-compose.yml`. The short version:

1. Comment out the entire `vpn` service block.
2. On the `haul` service, remove `network_mode: service:vpn`, remove `vpn` from `depends_on`, remove `extra_hosts`.
3. Add a `ports:` block to haul:
   ```yaml
   ports:
     - "${HAUL_PORT:-8484}:8484"
     - "${HAUL_PEER_PORT:-6881}:6881/tcp"
     - "${HAUL_PEER_PORT:-6881}:6881/udp"
   ```
4. The VPN entries in `.env` and `secrets/` can be left as-is.

## FlareSolverr

[FlareSolverr](https://github.com/FlareSolverr/FlareSolverr) is a Cloudflare challenge solver. It's useful if your indexers are behind Cloudflare's bot protection. Most users don't need it.

To start it:

```bash
docker compose --profile flaresolverr up -d
```

Then configure the URL in Pulse: Settings → FlareSolverr URL → `http://flaresolverr:8191`.

## Updating

```bash
docker compose pull        # pull latest images for all services
docker compose up -d       # recreate containers with new images
```

Each app handles its own database migrations on startup — no manual schema changes needed.

## Troubleshooting

**VPN won't connect**
- Check your credentials in `secrets/vpn-username.txt` and `secrets/vpn-password.txt` — no trailing newlines
- Verify the provider name is spelled correctly in `.env` (must match Gluetun's expected value exactly)
- Check Gluetun logs: `docker compose logs vpn`

**Haul can't reach Postgres or Pulse**
- This is usually a DNS resolution issue inside the VPN namespace. The `extra_hosts` entries on the vpn service inject `postgres` and `pulse` into `/etc/hosts` via `host-gateway`. Verify Docker 20.10+ is installed (`docker version`)
- Check that Postgres and Pulse ports are published on the host (they are by default)

**Database initialization failed**
- If the `pgdata` volume already exists from a previous run with different passwords, drop it: `docker compose down -v && docker compose up -d`
- Check Postgres logs: `docker compose logs postgres`

**Port conflicts**
- If another service already uses port 5432, 9696, 8383, 8282, or 8484, change the corresponding `*_PORT` variable in `.env`

**Permission errors on bind-mounted volumes**
- The Beacon app containers run as non-root users (UID 1000). Ensure the host directories in `DOWNLOADS_PATH`, `TV_PATH`, and `MOVIES_PATH` are writable by UID 1000, or adjust ownership: `sudo chown -R 1000:1000 ./data/`

## Privacy

No telemetry, no analytics, no crash reporting, no update checks. Every Beacon app makes outbound connections only to services you explicitly configure: TMDB for metadata, your indexers, your download clients, your media servers, and your VPN tunnel. Credentials stay in your local database and Docker secrets.

## License

MIT — see [LICENSE](LICENSE).
