# Real admin screenshots

Captured on 5 September 2026 from a running Fast Lane installation on
FriendlyWrt. These are not mockups and the server statuses and GET latency
values were not fabricated.

- [VPN](fastlane-vpn.png): combined pool, current GET results, and the server
  actually selected by Fast Lane.
- [Routing](fastlane-routing.png): direct routing for the selected local country
  and GeoIP/GeoSite readiness.
- [Settings](fastlane-settings.png): background intervals, URL test, automatic
  selection, language, updates, and application management.

## Privacy

Server addresses were replaced in the browser DOM immediately before the VPN
screenshot. Names, source counts, status, and latency were not changed. Browser
chrome, personal tabs, cookies, tokens, and router credentials are not present.
The temporary LuCI session used for the smoke test was destroyed afterwards.

## Verification boundary

The installed VPN page checksum matched the source used for this capture:
`63ddcda5a05faad5125003417f92dc40299e28edd1442a160fa65ab25aa4b765`.

The screenshots confirm visual rendering and the displayed state at capture
time. They do not replace runtime, failover, security, or cross-device tests.
See the [contributor rules](../../AGENTS.md) for the required verification.

Refresh these images from a real admin panel after visible interface changes,
and re-check them for sensitive data before publishing. Files in `design/` are
visual references, not evidence of a running implementation.
