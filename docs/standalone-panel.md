# Standalone Fast Lane panel

An experimental web interface embedded in the Fast Lane binary, served by the
existing optional management listener. No installed LuCI, Node.js runtime, CDN,
second VPN service, or separate networking coordinator is required. The existing
Fast Lane LuCI CSS, logo and icon renderer are mechanically exported into the
binary by `node scripts/sync-panel-design.cjs`. They remain the visual source of
truth; `TestPanelDesignMatchesLuCI` checks for export drift.

## Scope

- VPN status, actual route, auto/manual connection and disconnect.
- Subscription/file import, source removal, filtering and HTTPS health checks.
- Experimental AWG profiles in the common `Server List`: import one or several
  files via **Add servers → File → .conf**, then check/connect/hide/remove each
  profile from its row menu and disconnect through the common status bar. Stored
  secrets remain separate from subscriptions and do not enter automatic ranking.
- Original Routing screen: country selection and GeoIP/GeoSite, the route flow,
  named direct-exclusion groups, group editing/toggles/removal and HAPP preview.
- Original Settings controls: duration segments, keyword chips, URL checks,
  switch policy, language and application management. DNS editing remains an API/
  CLI capability; no new DNS card is inserted into the incumbent Settings screen.
- Original Diagnostics overview and expandable technical state, including files
  and runtime adapters on OpenWrt.

The source for Routing is mechanically exported by `sync-routing-panel.cjs`;
Settings/Diagnostics retain the original renderers and exported CSS. Adapter code
replaces LuCI transport, not product behavior. HAPP remains preview-only because
the existing LuCI screen also disables partial application. CLI and LuCI remain
available. No home-router deployment is performed by this change.

The panel reuses the original source tabs, status strip, table, row menus and
two-tab import dialog. Country and protocol filters, source/ping/name sorting,
hidden-server tab, row checks, hide/restore and individual-source refresh use the
existing service. AWG remains in the common list, not a separate card. Language
selection applies to the full panel and reloads the page; the initial language is
Russian, with English and browser-language selection available.
The local system font varies across operating
systems; no webfont or third-party asset is downloaded by the panel.

## Architecture and security

The browser submits commands to the running Fast Lane service. Runtime mutations
are serialized with daemon health passes and retain service/store locking. Update
downloads do not hold the health mutex. Explicit Geo maintenance uses the existing
indivisible helper and is serialized for its full duration, including validation,
reload and rollback; splitting its download/commit phases remains follow-up work. The tab
can close without cancelling the job. The job tracker is process-local: a daemon
restart loses its progress display; durable settings/runtime recovery remains
owned by the service, not by JavaScript.

Panel Geo maintenance runs the helper in the foreground, not its detached `start`
mode. Cancellation signals its owned process group and waits for transaction
cleanup before releasing the runtime lock. The work deadline is fifteen minutes;
rollback/cleanup may extend that wait rather than be forcibly killed halfway.

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
  -run '^TestStandalonePanelPreview$' -count=1 -timeout=13h -v
```

The test prints a loopback URL, labels its data as demonstration-only, and exits
after twelve hours. `FASTLANE_PANEL_PREVIEW_ADDR=127.0.0.1:56663` optionally keeps
a fixed loopback URL. Changes are discarded. Do not import real secrets into a preview.

These checks do not establish working VPN/AWG tunnels or NanoPi performance.
Router installation and traffic/failover tests remain separate acceptance steps.

## OpenWrt runtime acceptance

```sh
FASTLANE_RUN_OPENWRT_INTEGRATION=1 go test ./test/integration/openwrt \
  -run '^TestOpenWrtStandaloneAWGPanel$' -count=1 -timeout=25m -v
```

This exercises HTTP
import, check, connect, HTTPS egress, unchanged Xray PID, exclusion-group CRUD and
disconnect against a disposable QEMU guest with its own generated AWG server.
It never uses the home router or the user's VPN credentials.

On 2026-09-15 this scenario passed against OpenWrt 24.10.5, pinned Xray v26.7.28,
kernel AWG and a generated test-server namespace. The stand needs `dnsmasq-full`
for domain-exclusion nftset support. This confirms the tested panel/network path,
not a 24-hour soak, all failure cases or NanoPi performance.

To retain that **real** stand behind a loopback-only panel for twelve hours:

```sh
FASTLANE_RUN_OPENWRT_INTEGRATION=1 \
FASTLANE_STANDALONE_PREVIEW_ADDR=127.0.0.1:56663 \
  go test ./test/integration/openwrt \
  -run '^TestOpenWrtStandaloneAWGPanel$' -count=1 -timeout=13h -v
```

Wait for `Working isolated stand ready` before opening the address. Unlike the
store-only preview above, actions control the disposable OpenWrt guest and its
real test tunnel. A loopback proxy supplies the guest-only test credential and
enforces exact Host/Origin checks; browser login/logout is not the authentication
test in this mode. Guest changes are discarded when the stand stops. The tunnel
carries guest traffic only, never the host's or home network's traffic. Do not
import real credentials into the stand.

Interactive mode mounts a 128 MiB guest-memory volume for Geo databases because
the stock test image has a small root partition. This storage is disposable and
does not claim production upgrade capacity or reboot persistence.

The store-only macOS playground explicitly refuses VPN connection success without
a VPN backend. Geo updates, package management and uninstall require their actual
OpenWrt helpers; missing helpers are reported as unavailable, not simulated.
