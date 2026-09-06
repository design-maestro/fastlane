# Fast Lane architecture

## Runtime boundary

Fast Lane is a router service with three clients: LuCI, CLI, and TUI. The clients
submit commands and read persisted state. They do not own background refresh,
health checks, automatic selection, or failover.

On OpenWrt, `procd` runs `fastlane daemon`. The daemon survives browser navigation,
tab suspension, and a closed admin page. A manual GET check is queued in the
runtime directory and its progress can be read by any later LuCI session.

## Layers

### Domain

Subscriptions, nodes, settings, runtime state, node health, routing policy, and
selection results without UI or filesystem dependencies.

### Parser

Format detection and normalization for subscription URLs, YAML, Base64, Xray
JSON, and supported share links. Important Xray transport fields must survive
normalization.

### Store

Atomic JSON persistence under `/etc/fastlane`, guarded by a process lock and
restricted permissions. Corrupt state is quarantined rather than overwritten in
place.

### Application

Add, refresh, connect, disconnect, inspect, auto selection, expiry handling, and
the background scheduler. Long probes use a snapshot and commit their result only
if the inputs are still current.

### Probe and Xray backend

Candidate latency is a complete HTTPS GET through a temporary candidate Xray
outbound. It is not TCP-connect latency. Up to 10 candidates are checked in
parallel; every candidate has its own timeout. Results are published incrementally
and persisted for background status and future selection.

Before activating a candidate, Fast Lane validates the generated configuration
with `xray -test`. Failed candidates may fall through to another already-tested
candidate without replacing the last-known-good runtime.

### Router integration

Xray provides the proxy runtime. `nftables` applies transparent routing and source
selection. `dnsmasq` integrates router/LAN DNS. GeoIP covers direct routing for any
selected ISO country; explicitly supported GeoSite tags add domain routing without
making a missing country-specific tag a requirement.

## Automatic mode

1. Capture subscriptions, settings, and persisted runtime state.
2. Exclude expired subscriptions and nodes explicitly excluded from auto mode.
3. Check candidates with bounded parallel HTTPS GET requests.
4. Publish each finished result and select the best healthy candidate.
5. Respect failure thresholds, latency improvement, switch cooldown, and anti-flap policy.
6. Validate and activate the selected Xray configuration.
7. Persist the actual subscription, node, latency, health map, mode, and failure reason.

The daemon also verifies the active route. When the active GET fails, it starts an
immediate full reselection for the configured auto scope. Manual mode never moves
to another server on its own.

## Safety properties

- Browser lifetime does not control backend jobs.
- State writes are atomic and lock-protected.
- Stale probe results cannot overwrite newer user choices.
- Invalid Xray configuration cannot replace the last-known-good runtime.
- Expired subscriptions are view-only.
- Secret-bearing files use restrictive permissions.
- Runtime progress and transient locks live in RAM-backed `/tmp` on OpenWrt.
