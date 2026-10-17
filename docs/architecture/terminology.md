# Terminology: Browser Selection Concepts

Canonical reference for vocabulary used in browser selection, configuration,
and provider routing.

## Core terms

| Term            | Scope    | Definition |
|-----------------|----------|------------|
| **browser**     | Public   | The user-facing selection concept. Users choose a browser in config, CLI flags, and API calls. Replaces "engine" in all user-facing contexts. |
| **provider**    | Public   | One of `chrome | cloak | ghost-chrome`. The implementation behind a browser selection. Each provider maps to a distinct launch and routing strategy. |
| **static fetch**| Public   | The lightweight HTTP+DOM path used by `ghost-chrome` before escalating to Chrome. Replaces "lite engine" in user-facing language. |
| **engine**      | Removed  | No longer a public concept: `server.engine` in a config file fails validation. The old `chrome` / `lite` / `auto` values survive only as the internal `browsers.LaunchMode` (`internal/browsers/config.go`). |
| **target**      | Config   | A named launch profile under `browser.targets` in the config file (provider, binary path, proxy, flags, etc.), with `browser.defaultTarget` naming the default. Where a `browser` value is accepted (e.g. `POST /instances/attach`), a target name may be given instead of a provider. |

## Provider descriptions

### `chrome`

Full Chrome via CDP. Default provider. Launches a local Chromium-based browser
and connects over the Chrome DevTools Protocol.

### `cloak`

CloakBrowser — anti-detection Chrome fork. Uses a patched Chromium binary with
fingerprint randomization, timezone/locale spoofing, and WebRTC leak prevention.

### `ghost-chrome`

Static-first routing. Tries a lightweight HTTP fetch and DOM parse first;
escalates to a full Chrome session when the content is thin, dynamic, or
requires JavaScript execution.

## Migration notes

- The `engine` field in `ServerConfig` is no longer supported: it is parsed
  only so config validation can reject it. Use `browsers.default` in the
  config file instead. `RuntimeConfig` has no engine field.
- The `provider` field inside `browser {}` config blocks is no longer
  supported (validation error); use the top-level `browsers.default` key.
  Per-target `browser.targets.<name>.provider` is still required.
- Public documentation, CLI help text, and API responses should use
  "browser" and "provider" — never "engine" or "lite engine".
