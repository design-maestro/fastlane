# Fast Lane updates

## For users

In **Settings → Fast Lane update**:

1. Select **Check for updates** to see the installed version and the GitHub
   result.
2. If a stable release is available, open **What is new** and select
   **Install**.
3. Confirm the installation. Saved settings and subscriptions remain; VPN may
   reconnect briefly. Do not turn off the router.
4. Wait for completion, then reload the admin page.

The check and installation run as a separate router-side process. Closing the
browser tab does not cancel the task. Reopening LuCI shows persisted progress,
and repeated clicks cannot start parallel installations. Unsaved form changes
are never applied implicitly; save them before updating.

## Release channel

The only source is GitHub Releases from `design-maestro/fastlane`. Fast Lane does
not install branch builds, pull requests, drafts, or prereleases. Supported
stable tags use `v1.2.3`, with packages for `mipsel_24kc`, `x86_64`, and
`aarch64_cortex-a53`.

**The update button cannot work while the repository is private or before the
first stable release is published.** Fast Lane does not extract a GitHub token
from the browser or copy one from your computer to the router. The built-in
channel supports public releases only.

Scheduled installation is intentionally disabled: every update requires owner
confirmation. Background ping checks are a separate process.

## For maintainers

After an approved pull request and green checks, publish a
`vMAJOR.MINOR.PATCH` tag. The `Release` workflow reruns verification and builds
the packages. A usable release must contain `install.sh`, the Fast Lane archive
for the router architecture, and its `.sha256`; the GitHub API must also expose
a SHA-256 digest for every required asset. An incomplete release is not offered
for installation.

GitHub contract: [release and asset metadata](https://docs.github.com/en/rest/releases/releases).
Never reuse a published stable tag. The UI pins the candidate by release ID,
tag, asset IDs, and SHA-256 values. A changed release requires a fresh check and
confirmation. Confirmation expires after 30 minutes. Downgrades are rejected.

Commands:

```sh
fastlane --json update status
fastlane --json update check
fastlane --json update install --release 123456
```

The final command requires the real release ID returned by the check. `check`
and `install` enqueue work and return immediately; read the result through
`status` instead of submitting another installation.

## Security and limitations

- Fast Lane executes the installer attached to the pinned release, not the
  mutable `latest/download/install.sh` URL.
- HTTPS, redirect restrictions, timeouts, and size limits protect downloads.
- The installer and archive are checked against GitHub-provided SHA-256 values
  before execution. This proves integrity relative to the trusted GitHub
  channel; it is not an independent author signature.
- An archive may not contain links, path traversal, secrets, or files outside
  Fast Lane.
- The verified archive is passed to the installer locally; a missing file is
  not replaced with a new network download.
- Files are replaced by rename, so a running binary is never overwritten in
  place.
- The new binary version is verified after installation. Failures are not
  reported as success.
- Job state and temporary assets live in `/tmp/fastlane-update` with restricted
  permissions. No persistent update log is streamed to flash, and temporary
  files are removed after completion.

Per-file replacement is not a full-system transaction and cannot guarantee a
complete rollback after power loss. Back up an important router before updating.
Validate every release with a separate install/upgrade smoke test on relevant
hardware.
