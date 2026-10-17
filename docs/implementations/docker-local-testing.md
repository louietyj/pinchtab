# Docker Local Testing

This page is a practical checklist for testing the current Docker setup locally.

It covers two paths:

- the default managed-config flow, where the container owns `/data/.pinchtab/config.json` (`HOME=/data`; on Linux the default config lives under `~/.pinchtab`)
- the explicit-config flow, where you mount your own `config.json` and set `PINCHTAB_CONFIG`

## Managed Config Flow

Build and start the local Compose service:

```bash
docker compose up --build -d
docker compose logs -f pinchtab
```

Inspect the effective config path and persisted config:

```bash
docker exec pinchtab pinchtab config path
docker exec pinchtab sh -lc 'cat /data/.pinchtab/config.json'
```

Expected results:

- the config path is `/data/.pinchtab/config.json`
- `server.bind` in the persisted config is `0.0.0.0` (the entrypoint sets it so port publishing works)
- a token is present if one was generated on first boot or passed in

Verify the config bind address:

```bash
docker exec pinchtab pinchtab config get server.bind
```

Expected result: `0.0.0.0` (set by entrypoint on first boot)

Verify persistence across restart:

```bash
docker compose down
docker compose up -d
docker exec pinchtab sh -lc 'cat /data/.pinchtab/config.json'
```

## Explicit `PINCHTAB_CONFIG` Flow

Create a local config file, for example `./tmp/config.json`:

```json
{
  "server": {
    "bind": "0.0.0.0",
    "port": "9867",
    "token": "local-test-token"
  }
}
```

Run the container with that config mounted read-only (to test a local build instead of the published image, first run `docker build -t pinchtab/pinchtab .`):

```bash
docker run --rm -d \
  --name pinchtab-test \
  -p 127.0.0.1:9867:9867 \
  -e PINCHTAB_CONFIG=/config/config.json \
  -v "$PWD/tmp/config.json:/config/config.json:ro" \
  -v pinchtab-data:/data \
  --shm-size=2g \
  pinchtab/pinchtab
```

Verify the explicit config path and auth:

```bash
docker exec pinchtab-test pinchtab config path
docker exec pinchtab-test sh -lc 'cat /config/config.json'
curl -H 'Authorization: Bearer local-test-token' http://127.0.0.1:9867/health
```

Expected results:

- `pinchtab config path` reports `/config/config.json`
- the mounted file is used as-is
- the container entrypoint does not rewrite the custom config

## What To Check When Something Fails

Container logs:

```bash
docker logs pinchtab
docker logs pinchtab-test
```

Config path:

```bash
docker exec pinchtab pinchtab config path
docker exec pinchtab-test pinchtab config path
```

Persisted config content:

```bash
docker exec pinchtab sh -lc 'cat /data/.pinchtab/config.json'
```

## Automated Docker E2E

The E2E stack (`tests/e2e/docker-compose.yml`, `tests/e2e/docker-compose-multi.yml`) builds this same `Dockerfile` and runs `docker-entrypoint.sh`, but always with `PINCHTAB_CONFIG` set — so it exercises the explicit-config flow only. The managed-config flow above is covered only by this manual checklist.

```bash
./dev e2e                     # extended suite (go run ./tests/tools/runner e2e --suite extended)
./dev e2e smoke               # smoke tier
./dev e2e api <text>          # one suite, filtered by scenario file name (--suite api --filter <text>)
./dev e2e test "<name>"       # runs only the first start_test whose name contains <name>
```

## Current Caveats

The Docker runtime path owns `--no-sandbox` compatibility now (added at launch when a container is detected). Do not put it in `browser.extraFlags`; config validation rejects it.

`docker-entrypoint.sh` checks for an existing config at `$XDG_CONFIG_HOME/pinchtab/config.json` (`/data/.config/...`), but the binary reads and writes `/data/.pinchtab/config.json`, so the "first boot" block runs on every start: `server.bind` is re-set to `0.0.0.0` and, when `PINCHTAB_TOKEN` is set, `server.token` is overwritten with it.
