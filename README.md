# Beacon Stack — deploy (retired)

**This repo is archived.** Each Beacon app now ships its own Docker Compose
file, like Sonarr and Radarr, and the website shows how to connect them.

**Start here: <https://beaconstack.io/install>**

| App | Compose file |
|---|---|
| Pilot (TV) | [beacon-stack/pilot `docker/docker-compose.yml`](https://github.com/beacon-stack/pilot/blob/main/docker/docker-compose.yml) |
| Prism (movies) | [beacon-stack/prism `docker/docker-compose.yml`](https://github.com/beacon-stack/prism/blob/main/docker/docker-compose.yml) |
| Haul (BitTorrent) | [beacon-stack/haul `docker/docker-compose.yml`](https://github.com/beacon-stack/haul/blob/main/docker/docker-compose.yml) |
| Pulse (optional hub) | [beacon-stack/pulse `docker-compose.yml`](https://github.com/beacon-stack/pulse/blob/main/docker-compose.yml) |

## Moving off this repo

The compose file here still works, but it won't get updates. To switch:

1. Stop the stack: `docker compose down`.
2. Move each app's config folder (`./config/<app>` in older versions of
   this repo, `$BEACON_CONFIG/<app>` in newer ones) to wherever you want
   it, e.g. `/opt/beacon/<app>`.
3. Use the full-stack example from <https://beaconstack.io/install>,
   pointing each app's `/config` at the moved folder and `/data` at your
   media folder.
4. If your library or downloads paths change, update the library path in
   Pilot/Prism (Settings → Libraries).

Your databases, settings and API keys move with the config folders.
