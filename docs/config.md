# Fast Lane configuration

## Paths

OpenWrt:

- `/etc/fastlane/subscriptions.json`
- `/etc/fastlane/settings.json`
- `/etc/fastlane/state.json`
- `/etc/xray/config.json`
- `/tmp/fastlane-runtime/health-check.request`
- `/tmp/fastlane-runtime/health-check-progress.json`

An optional management access token may be stored at
`/etc/fastlane/management.token`. It must be readable only by its owner (`0600`).

Local development uses `./.fastlane/`. Persistent Fast Lane directories use
`0700`; state, locks, live Xray config, and last-known-good config use `0600`.

## Main settings

| Setting | Purpose |
| --- | --- |
| `refresh_interval` | Subscription refresh cadence. |
| `health_check_interval` | Background auto-mode check cadence. |
| `url_test_url` | Absolute HTTPS URL fetched through every candidate outbound. |
| `url_test_timeout` | Per-candidate GET timeout, not a timeout for the whole list. |
| `switch_cooldown` | Minimum time between healthy-node switches. |
| `latency_threshold` | Minimum improvement required to replace a healthy node. |
| `auto_mode` / `mode` | Automatic selection or manually pinned server. |
| `strict_egress_check` | Reject a selected runtime if its verification GET fails. |
| `country_routing.enabled` | Send LAN and the selected local country directly. |
| `country_routing.country_code` | ISO 3166-1 alpha-2 country code used by the direct GeoIP rule. |
| `log_level` | `debug`, `info`, `warn`, or `error`. |

The default URL test is `https://www.gstatic.com/generate_204`; the default
per-server timeout is 5 seconds. Checks are bounded to 10 concurrent candidates,
so a list larger than 10 completes in waves. A wave is approximately the slowest
candidate in that wave plus Xray setup overhead, not `timeout × server count`.

## Background behavior

Background work requires `/etc/init.d/fastlane` to be enabled and running. The
daemon persists health and actual active-node state, so LuCI can show fresh data
after being closed. Manual LuCI checks are queued to the same service and continue
after navigation.

Expired subscriptions remain stored and visible. They are skipped by refresh,
GET checks, manual connection, and automatic selection until valid metadata is
imported again.

## Optional management HTTP API

Fast Lane can expose the existing application service on a separate TCP port.
This is an API-only foundation for a future standalone Fast Lane interface; it
does not bundle or copy another project's UI and is disabled by default.

Loopback development does not require a token:

```sh
fastlane daemon --management-listen 127.0.0.1:9080
```

Any LAN or wildcard bind requires an access token of at least 32 characters in
a `0600` file. The listener currently uses plain HTTP, so expose it only on a
trusted LAN or through an authenticated tunnel; do not port-forward it to the
internet.

```sh
umask 077
head -c 32 /dev/urandom | base64 > /etc/fastlane/management.token
FASTLANE_MANAGEMENT_LISTEN=192.168.1.1:9080 \
FASTLANE_MANAGEMENT_TOKEN_FILE=/etc/fastlane/management.token \
/etc/init.d/fastlane restart
```

The packaged init script accepts those two per-start environment overrides. The
listener uses the same Fast Lane state and VPN runtime as LuCI/CLI/TUI.
Long-running requests are serialized with daemon health passes, return
`202 Accepted`, and continue after the requesting tab closes.
`GET /api/v1/state` exposes status,
API-safe subscriptions, nodes, and current job state. Authentication supports a
Bearer token or a strict `HttpOnly` session cookie created through
`POST /api/v1/session`.

## GeoIP and GeoSite

The geodata helper installs and validates databases separately from the Fast Lane
binary. Enabling `country_routing` changes generated Xray routing only after the
required assets are present and the configuration passes validation. Every ISO
country uses `geoip:<code>`. Fast Lane adds a GeoSite rule only for country tags it
explicitly supports and still validates the complete Xray configuration before
replacing the active runtime. Schema 10 `russia_direct` values migrate to country
`RU` without changing their enabled state.

## Logs and flash wear

Transient progress is stored under `/tmp`, which is RAM-backed on normal OpenWrt
systems. Xray's file log is truncated hourly by the managed cron entry. Corrupt
state backups older than seven days are removed daily. Fast Lane's service output
uses the OpenWrt system log, which is normally a bounded RAM ring buffer.

Do not move high-frequency progress or debug logs to persistent flash unless you
also add explicit size and retention limits.

## Upgrade and recovery

- In-place updates preserve `/etc/fastlane`.
- Older schema versions migrate during load.
- Malformed settings or state are renamed to `*.corrupt-<UTC>.json` and replaced with canonical state.
- Future schema versions are never silently downgraded.
- The installer records dependencies it added; uninstall removes only recorded Fast Lane-owned components.
