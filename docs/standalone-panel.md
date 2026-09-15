# Standalone Fast Lane panel

An experimental web interface embedded in the Fast Lane binary, served by the
existing optional management listener. No LuCI assets, Node.js runtime, CDN,
second VPN service, or separate networking coordinator is required.

## Scope

- VPN status, actual route, auto/manual connection and disconnect.
- Subscription/file import, source removal, filtering and HTTPS health checks.
- One experimental AWG profile: import, check, connect, disconnect, remove.
- Split/bypass routing and excluded LAN devices; explicit confirmation before applying.
- DNS, keyword hiding, refresh and mass-check intervals; unsaved forms survive polling.
- Diagnostics, active outbound and reserve timestamps.

This is not yet full LuCI parity: this prototype is Russian-only, uses simple
selector forms instead of named exception groups, and does not provide the
release installer, Geo database administration, language selection or all legacy
routing editors. Existing `hosts`/`targets` routing is preserved and labelled;
the user must explicitly choose a replacement mode to change it. Existing CLI
and LuCI remain available. No home-router deployment is performed by this change.

The compact prototype also uses a source dropdown, an inline import form, plain
duration inputs and a keyword textarea. Country flags, latency sorting, per-row
manual hiding and the LuCI chip/group editors are not ported yet. Existing manual
exclusions remain respected. The local system font varies across operating
systems; no webfont or third-party asset is downloaded by the panel.

## Architecture and security

The browser submits commands to the running Fast Lane service. Mutating jobs are
serialized with daemon health passes and retain service/store locking. The tab
can close without cancelling the job. The job tracker is process-local: a daemon
restart loses its progress display; durable settings/runtime recovery remains
owned by the service, not by JavaScript.

The listener is opt-in. See [configuration](config.md#optional-management-http-api)
for start flags. Do not run a second daemon against an already active store.
The documented OpenWrt environment overrides apply to that start only; they do
not constitute a persistent UI-enablement setting across reboots.

LAN binds require a token of at least 32 characters. Browser login creates an
individual random HttpOnly, SameSite=Strict cookie with a server-enforced 12-hour
lifetime. Logout revokes that session only; a daemon restart revokes all sessions.
No token is placed in a URL, bundle or browser storage. Assets are public, but
state and commands require authentication. Same-origin checks, login rate
limits and a restrictive CSP apply. Secrets use the existing redacted API.

The built-in listener is HTTP, not HTTPS. A key and SameSite cookie do not protect
against an untrusted network observer. Use a trusted local connection or an SSH
tunnel; do not expose the listener to WAN. TLS termination is a separate setup,
not automatically installed by this prototype.

## Additional endpoints

All mutations return the existing asynchronous job envelope (`202`); state is
read from `GET /api/v1/state`. `DELETE /api/v1/session` revokes browser login.

| Endpoint | Method | Payload |
| --- | --- | --- |
| `/api/v1/jobs/settings` | POST | Existing supported settings patch |
| `/api/v1/hide-keywords` | POST | `keywords` array |
| `/api/v1/dns` | POST | `mode`, `transport`, `servers`; preserves bootstrap/local domains |
| `/api/v1/routing` | POST | `mode` (`bypass`, `split`, `disabled`), `proxy`, `bypass`, `excluded` arrays |
| `/api/v1/awg` | GET / POST / DELETE | Import: `name`, `config`; GET returns redacted status |
| `/api/v1/awg/check` | POST | No payload |
| `/api/v1/awg/connect` | POST | No payload |
| `/api/v1/awg/disconnect` | POST | No payload |

## Reproduce UI tests

```sh
make lint
make coverage-runtime
go test -race ./internal/managementhttp
FASTLANE_PANEL_BROWSER=1 FASTLANE_PANEL_SCREENSHOTS=/tmp/fastlane-panel-review \
  go test ./internal/managementhttp -run TestStandalonePanelBrowser -count=1 -v
```

Browser tests use installed Chrome and a real service with a disposable store,
without Xray, AWG or host-network controllers. They verify authentication,
subscription import, logout, focus preservation, unsaved fields and responsive
layout. API tests verify session revocation/expiry, unauthorized state access,
settings/DNS/routing persistence and rejection of unsafe AWG input.

For a local playground (synthetic profile; no network controller), run:

```sh
FASTLANE_PANEL_PREVIEW=1 go test ./internal/managementhttp \
  -run '^TestStandalonePanelPreview$' -count=1 -timeout=65m -v
```

The test prints a loopback URL, labels its data as demonstration-only, and exits
after an hour. Changes are discarded. Do not import real secrets into a preview.

These checks do not establish working VPN/AWG tunnels or NanoPi performance.
Router installation and traffic/failover tests remain separate acceptance steps.
