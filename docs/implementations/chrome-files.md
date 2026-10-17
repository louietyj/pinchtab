# Chrome User Data Directories & Profiles

PinchTab manages Chrome instances using dedicated **User Data Directories** (profiles). This document explains how these directories are resolved, how locks are managed, and how to handle parallel browser instances.

## Profile Resolution

PinchTab derives the Chrome `--user-data-dir` (profile) as `profiles.baseDir` joined with `profiles.defaultProfile` (`finalizeProfileConfig` in `internal/config/config_load.go`):

1.  **Named Profile**: When an instance is started for a named profile (dashboard, `pinchtab instance start --profile <name>`, or the API), the orchestrator writes the child's config with `profiles.baseDir` / `profiles.defaultProfile` pointing at that profile's directory (`buildChildFileConfig` in `internal/orchestrator/child_config.go`).
2.  **Default Profile**: Otherwise `profiles.baseDir` defaults to `<server.stateDir>/profiles` and `profiles.defaultProfile` to `default`, giving `~/.pinchtab/profiles/default` on Linux/macOS.

There is no config key for an arbitrary profile path, and `--user-data-dir` in `browser.extraFlags` is rejected by config validation.

## The Singleton Model

Chrome enforces a **Singleton** model per User Data Directory. This means:
*   Only **one** Chrome process can use a specific profile directory at any given time.
*   If a second process attempts to use the same directory, Chrome will either fail to start or (in some configurations) attempt to open a new window in the existing process.

PinchTab adds an additional layer of protection using a `pinchtab.pid` file within the profile directory to ensure that only one PinchTab instance manages a specific profile.

## New Headless & Parallel Instances

With the introduction of **New Headless mode** (`--headless=new`), Chrome's profile sharing behavior has become stricter:
*   **No Sharing**: You cannot reuse the same `--user-data-dir` across multiple concurrent instances.
*   **Separate Directories Required**: Parallel browsers **must** use separate directories to avoid random startup errors and lock conflicts.

### Headless Auto-Fallback
To support parallel automation tasks, PinchTab implements an **automatic fallback for headless instances**:
1.  If a headless instance tries to start using a profile that is already locked by another PinchTab process, it will **automatically create a unique temporary directory** (`pinchtab-profile-*` under the system temp directory, e.g. `/tmp`).
2.  This allows you to run multiple headless tasks in parallel without manually managing profile paths.

### Manual Parallelism (Headed Mode)
In **headed mode**, PinchTab does *not* automatically fall back to a temporary directory (to avoid losing user session data unexpectedly). If you need to run multiple headed browsers in parallel, you must:
*   Use a different named profile for each instance, or
*   point `profiles.baseDir` / `profiles.defaultProfile` at a separate directory for each server.

## Best Practices for AI Agents

When building agents that use PinchTab, follow these guidelines:

*   **Persistence**: Use named profiles (e.g., `agent-alpha`, `agent-beta`) if you need the browser to remember logins, cookies, or history across sessions.
*   **Isolation**: For one-off tasks or high-concurrency scraping, rely on the default headless mode which handles directory isolation automatically if conflicts occur.
*   **Cleanup**: If you manually create temporary directories, ensure they are cleaned up after the task is complete to avoid filling up disk space.

## Troubleshooting

If you see the error `"The profile appears to be in use by another Chromium process"`:
1.  **Check for active instances**: Ensure you don't have another PinchTab or Chrome process already using that profile.
2.  **Stale Locks**: If no process is active, PinchTab automatically clears the stale `SingletonLock` / `SingletonSocket` / `SingletonCookie` files when browser startup fails with this error, then retries once.
3.  **Manual Fix**: In rare cases, you may need to manually remove the `SingletonLock` file from the profile directory.

For more details on how PinchTab recovers from crashes, see [Chrome Profile Lock Recovery](./chrome-profile-lock-recovery.md).
