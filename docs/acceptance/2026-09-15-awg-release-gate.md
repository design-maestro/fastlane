# Fast Lane: AWG release acceptance, 2026-09-15

Status: **SHORT RELEASE GATES PASSED — 24-HOUR RUN CANCELLED BY USER**.

## Exact scope

- Candidate baseline: `d9e95e4` plus this acceptance patch, branch `feat/standalone-web-panel`.
- Test build label: `0.0.0-test.d9e95e4`, Go 1.26.6, Linux ARM64 and AMD64.
- Isolated OpenWrt 24.10.5 x86_64, Xray v26.7.28, test-generated AWG keys.
- No home-router changes, no user's profile upload, no stable release/tag.

Builds are local in `dist/acceptance-d9e95e4/` and are not installation approval.

| Binary | SHA-256 |
| --- | --- |
| linux-arm64/fastlane | d461ba3dd7e2e5b9e8ff190a5bed08b2373b20ecb1a584b9f4a6baf7a11d2d43 |
| linux-amd64/fastlane | 3e1bc41052cc2a3b12d7e78e26c586d4bcf49fa62a68eef9d7255888515bb659 |

## Evidence from this acceptance run

| Check | Result |
| --- | --- |
| ARM64 and AMD64 static test builds | PASS; ELF architecture verified |
| `make lint` and `make coverage-runtime` | PASS |
| Existing LuCI runtime smoke | PASS, 67/67 |
| Real pinned-Xray managed TCP/UDP switching test | PASS; existing TCP and PID preserved |
| Race tests: app, AWG, HTTP, CLI | PASS |
| Real OpenWrt AWG failure/direct/recovery scenario | PASS, 354.35 seconds total |
| Real AWG to working VLESS reserve | PASS; independent two-site VLESS HTTPS, AWG -> verified VLESS in 17.513s, direct fallback, daemon restart and cold boot passed in 496.07s |
| Published `v0.1.24` -> candidate `v0.1.25` archive path | PASS; injected late failure restored files/modes/existence/service, then upgrade, cold boot, settings/API and downgrade rejection passed in 420.21s |

The earlier UI/HTTP stand run is documented in `../standalone-panel.md`. It is
separate evidence, not a substitute for the failure trials above.

The AWG-only run measured 2.151 seconds for the already-verified hot switch,
15.325 seconds from AWG failure to direct mode, and 16.846 seconds from restored
server to AWG recovery. Xray/dnsmasq PIDs were unchanged. UDP, HTTPS and explicit
direct-exclusion assertions passed. This first run started before the stronger
DNS assertion was compiled; the reserve acceptance run uses the corrected check.
The QEMU throughput sample was 0.74 Mbit/s under this test environment and is not
a NanoPi throughput result.

## Test quality correction

The old AWG DNS command discarded `dig` output and returned the exit code of
route cleanup. A DNS failure could therefore pass. The assertion now preserves
the query result and requires an actual IPv4 answer. The direct-fallback phase
also checks HTTPS and router DNS rather than trusting only `state.json`.
Earlier DNS claims must not be treated as proof of these stronger assertions.

## Target-device gates deferred until installation

- Exact target NanoPi firmware/kernel ABI and matching AWG module must be verified;
  x86_64 OpenWrt package success does not establish ARM64/FriendlyWrt compatibility.
- GeoIP/GeoSite routing and AWG kernel compatibility must still be observed on
  the actual NanoPi before AWG is enabled there.
- The user cancelled the 24-hour VM soak and chose to perform extended testing
  after release. Short QEMU samples do not establish NanoPi performance or
  24-hour stability.
- CI is blocked on the PR author's CLA acknowledgment. It was not accepted on
  behalf of the user, and the gate must not be removed or bypassed.

Independent packaging review found release limitations. The candidate now adds
a rollback journal for ordinary installer failures:

- Injected failures after partial file publication and during service restart
  restore previous contents, modes, links, new-file absence and service state in
  nine automated scenarios. A real published `v0.1.24` archive trial also passed
  rollback, candidate installation, reboot and downgrade rejection. Package-manager
  changes, SIGKILL and power loss remain outside this guarantee.
- The missing `dnsmasq-full` IPK dependency was corrected in this acceptance
  patch and the generated-control regression test was updated. No package was
  installed on a home router. Existing `--without-deps` upgrade paths still need
  a real target preflight; declaring a dependency does not repair an existing
  installation whose dependencies were deliberately skipped.
- QEMU's ordinary `InstallFastLane` helper still copies files directly, but the
  dedicated archive test uses the published `v0.1.24` installer/assets and the
  real candidate archives. Local stable comparison: `v0.1.24` (`0dae6af`).

`test/release`, `internal/update`, `internal/amneziawg`, and selected CLI upgrade
tests passed in the review. They cover their test contracts, not the missing
target-device/real-installer scenarios above.

Geo maintenance is currently an explicit serialized operation: the runtime lock
is retained through helper cleanup. It can delay health checks. A timed-out helper
is not permitted to continue detached after that lock is released.

## Cancelled 24-hour isolated run

The user initially approved a 24-hour VM run. The first setup attempt at
2026-09-15 16:22:18 UTC was deliberately terminated before readiness to correct
a test race: minute samples could fail during an allowed controlled transition.
Its failed report is retained as `awg-soak-24h.json`; it is not a runtime-failure
finding or a completed soak. The corrected run uses the candidate binary above.
Preparation is not
part of the 24-hour interval: `soak_started_at` and `deadline` in the live report
are authoritative once readiness and the initial real HTTPS/DNS sample pass.

- Live report for the corrected run: `dist/acceptance-d9e95e4/awg-soak-24h-v2.json` (0600).
- Test: `TestOpenWrtAWGSoak`, separate disposable OpenWrt guest and generated keys.
- Minute samples: actual proxy HTTPS, positive router DNS A answer, actual Xray
  balancer selection, manual state, unchanged Xray/dnsmasq PIDs.
- Every two hours: AWG outage, verified direct connectivity, automatic AWG return.
- A fresh daemon PID and ready local HTTP API are required after a warm restart
  before the soak timer starts. Long host sleep/stale samples do not count as a pass.
- `caffeinate -i` temporarily prevents idle sleep only while the test runs.
- Hourly thread heartbeat `fast-lane-awg` checks the report and live process;
  notifications are limited to completion, failure or an actionable interruption.

The corrected run reached steady AWG operation and completed 17 minute
HTTPS/DNS/PID samples over 1,003 seconds, then the user explicitly cancelled it before 24 hours and
chose to perform extended testing after release. The test process was terminated
and its hourly heartbeat paused. This run is **CANCELLED**, not passed, and must
not be cited as 24-hour stability evidence. It does not cover NanoPi performance.

## NanoPi compatibility preflight

The router login page reports FriendlyWrt/OpenWrt `25.12.5 r33051-f5dae5ece4`,
but unauthenticated `ubus system board` access is correctly denied, so the model,
target and running kernel are not yet verified. The AWG package release does have
25.12.5 assets, but this is insufficient: upstream issue #162 records NanoPi R5S
on the same OpenWrt revision with kernel 6.18.40 while the available Rockchip
module depended on kernel 6.12.94. This exact mismatch must fail closed.

Fast Lane's OpenWrt AWG controller already checks the running kernel, installed
module path and the package manager's exact kernel ABI dependency. It never uses
forced module loading. The target gate therefore requires authenticated read-only
output from `ubus call system board`, `uname -r`, and package metadata before any
AWG package installation is attempted.
