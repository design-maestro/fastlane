## What changed

<!-- Briefly describe the user-visible result. -->

## Why

<!-- Link an issue or explain the problem. -->

## Verification

- [ ] `make lint`
- [ ] `make coverage-runtime`
- [ ] `node test/luci/runtime_smoke.js .` — when LuCI changed
- [ ] Tested on real OpenWrt/FriendlyWrt — when runtime, installation, or routing changed

Device and firmware version:

## UI

<!-- For visible changes, attach desktop and mobile before/after screenshots. -->

## Risks

<!-- What could regress and what remains unverified. -->

## Security

- [ ] The changes contain no backups, subscriptions, UUIDs, keys, tokens, cookies, public IPs, or home-device data.
- [ ] `make security-check` passes.

## Contributor terms

- [ ] I have read and agree to the [Fast Lane Contributor License Agreement](../CONTRIBUTOR_LICENSE_AGREEMENT.md), and I have the right to submit this contribution.
