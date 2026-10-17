# Profile Management

When running `pinchtab`, profiles are managed via the HTTP API on port 9867.

## List profiles

```bash
curl http://localhost:9867/profiles
```

Returns array of profiles with `id`, `name`, `accountEmail`, `useWhen`, etc.

## Start a profile

```bash
# Auto-allocate port (recommended)
curl -X POST http://localhost:9867/profiles/<ID>/start

# With specific port and headless mode
curl -X POST http://localhost:9867/profiles/<ID>/start \
  -H 'Content-Type: application/json' \
  -d '{"port": "9868", "headless": true}'
```

Returns instance info including allocated `port`. Use that port for all subsequent API calls.

## Stop a profile

```bash
curl -X POST http://localhost:9867/profiles/<ID>/stop
```

## Check instance status

```bash
# By profile ID (recommended)
curl http://localhost:9867/profiles/<ID>/instance

# By profile name
curl http://localhost:9867/profiles/My%20Profile/instance
```

## Start by existing profile

```bash
curl -X POST http://localhost:9867/profiles \
  -H 'Content-Type: application/json' \
  -d '{"name": "work"}'

curl -X POST http://localhost:9867/instances/start \
  -H 'Content-Type: application/json' \
  -d '{"profileId": "work", "port": "9868"}'
```

## CLI usage with profiles

CLI subcommands are available — use these instead of `curl` when possible:

```bash
pinchtab profiles                                   # list profiles
pinchtab profiles create <name>                     # human setup: create before authentication
pinchtab instance start --profile <name>            # start (auto-allocates port)
pinchtab instance navigate <id> <url>               # open a tab already on <url> (one step)
pinchtab instance stop <id>
pinchtab instance restart <id>
pinchtab instance logs <id>
```

Creating a profile only creates an empty browser state. A human must start it headed and
authenticate before an agent can reuse that session.

Once a profile instance is running, the server routes to the instance that owns the tab (or the first running one); you can also target it explicitly:

```bash
pinchtab --server http://localhost:9868 snap -i
```

## Typical agent flow

```bash
# 1. List profiles
PROFILES=$(curl -s http://localhost:9867/profiles)

# 2. Start profile (auto-allocates port)
INSTANCE=$(curl -s -X POST http://localhost:9867/profiles/$PROFILE_ID/start)
PORT=$(echo $INSTANCE | jq -r .port)

# 3. Use the instance
curl -X POST http://localhost:$PORT/navigate -H 'Content-Type: application/json' \
  -d '{"url": "https://mail.google.com"}'
curl http://localhost:$PORT/snapshot?maxTokens=4000

# 4. Stop when done
curl -s -X POST http://localhost:9867/profiles/$PROFILE_ID/stop
```

## Profile IDs

Each profile gets a stable ID of the form `prof_XXXXXXXX` (`prof_` + the first 8 hex chars of SHA-256 of the name) stored in `profile.json`. IDs are URL-safe and never change — use them instead of names in automation.

## Headed mode

Headed mode = real visible Chrome window managed by Pinchtab.

- Human completes sign-in and any verification step, then validates state
- Agent calls HTTP APIs against the same running instance
- Session state persists in profile directory (cookies/storage carry over)

Recommended human + agent flow:

```bash
# Human starts the PinchTab server and sets up the profile
pinchtab server

# Agent resolves the profile's running instance
PORT=$(curl -s http://localhost:9867/profiles/<profile-name>/instance | jq -r .port)
curl "http://localhost:$PORT/health"
```
