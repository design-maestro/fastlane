# Fast Lane agent rules

These instructions apply to every AI coding agent working in this repository.

## Source of truth

1. `PRODUCT.md` defines product intent and user-facing behavior.
2. `DESIGN.md` defines the visual direction and interaction rules.
3. Existing runtime code and tests define current technical behavior.
4. If these disagree, do not silently guess. Explain the conflict in the pull request.

## Non-negotiable rules

- The product name is **Fast Lane**. Do not introduce former project names in code, UI, docs, assets, package names, paths, or migration messages.
- Never commit router backups, real subscription payloads, UUIDs, private keys, access tokens, cookies, public IP inventories, or files copied from `/etc/fastlane` or `/etc/xray`.
- Do not inspect files under `backups/`; the directory is intentionally ignored and may contain real credentials.
- Do not run `git add -A`. Stage explicit paths and run `make security-check` before committing.
- Preserve unrelated user changes in a dirty working tree.
- Never disable or bypass `xray -test`, atomic writes, the store lock, permission hardening, or last-known-good recovery to make a test pass.
- Browser code must not own long-running work. LuCI queues an operation and polls persisted backend progress; closing or switching the page must not cancel it.
- Health latency means a real HTTPS GET through the candidate Xray outbound. A TCP handshake is not a substitute.
- Candidate checks stay bounded and parallel. The current global limit is 10; changing it requires load evidence and tests.
- Background auto mode must persist fresh health and the actually active server so every interface reads the same state.
- Expired subscriptions remain viewable but do not refresh, run GET checks, connect, or participate in automatic selection.
- GeoIP/GeoSite routing must fail safely. Do not replace working runtime state with an invalid configuration or incomplete databases.

## UI rules

- Use the dark operational-dashboard direction in `DESIGN.md` and the supplied reference image.
- Keep `VPN`, `Routing`, `Diagnostics`, and `Settings` as the main navigation,
  translated through LuCI rather than hard-coded per page.
- One active server, one actual VPN status, and one selection mode must be unambiguous.
- Status must include text or an icon; color alone is insufficient.
- Desktop and mobile are both required. Mobile must not introduce horizontal page scrolling.
- Reuse the existing Fast Lane CSS tokens and components before adding another visual system.
- Destructive actions require clear wording and confirmation.
- Do not add speculative features or decorative UI without approved behavior.

## Required checks

For backend or shared-contract changes:

```sh
make lint
make coverage-runtime
```

For LuCI changes, also run:

```sh
node test/luci/runtime_smoke.js .
```

Review the affected desktop and mobile states in a real LuCI instance whenever
router behavior or layout is changed. State exactly what was and was not tested
in the pull request.
