# Contributing to Fast Lane

Thank you for helping. The safest path is a separate branch, a pull request,
green checks, and repository-owner review before a change reaches `main`.

## Two ways to contribute

### If you have repository access

1. Create a branch from the latest `main`: `feature/<short-name>` or
   `fix/<short-name>`.
2. Make one related group of changes.
3. Run the required checks.
4. Push the branch and open a pull request into `main`.

### If you do not have repository access

Fork the repository on GitHub, create a branch in your fork, and open a pull
request into `design-maestro/fastlane:main`. You do not need direct write access
to the original repository.

## Before you start

Read [PRODUCT.md](PRODUCT.md), [DESIGN.md](DESIGN.md), and
[AGENTS.md](AGENTS.md). Avoid mixing product logic, visual redesign, and network
runtime changes without a clear reason; such pull requests are harder to review
safely.

## Required checks

```sh
make lint
make coverage-runtime
node test/luci/runtime_smoke.js .
```

For network, installer, and LuCI changes, include evidence from real
OpenWrt/FriendlyWrt in the pull request: what you tested, on which device and
firmware, and what remains unverified. Green unit tests alone do not prove that
routing works on a router.

## Data safety

Never add:

- router archives or backups;
- content copied from `/etc/fastlane`, `/etc/xray`, or `/etc/config`;
- real subscriptions, UUIDs, keys, tokens, cookies, public IP inventories, or
  home-device lists;
- logs that still contain addresses or secrets.

Run `make security-check` before committing. If you find a vulnerability, follow
the [security policy](SECURITY.md) instead of opening a public issue.

## Pull requests

- Describe the user outcome, not only the files changed.
- Attach desktop and mobile screenshots for visible changes.
- List the checks you ran.
- Do not mix repository-wide formatting with a functional change.
- Address review comments and wait for green CI.

A change reaches `main` only after review and merge. The owner does not need to
recommit somebody else's code: GitHub preserves authorship, discussion, and CI
results when the pull request is merged.
