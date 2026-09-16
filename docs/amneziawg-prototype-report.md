# AmneziaWG prototype report

## Scope

This prototype adds one manually managed AmneziaWG Legacy or 2.0 profile to Fast Lane. It is intentionally separate from Xray subscriptions and does not participate in automatic speed-based server selection.

The prototype was exercised on an isolated OpenWrt 24.10.5 x86_64 QEMU stand with Xray v26.7.28 and a kernel AmneziaWG interface managed by netifd. It has not been installed on the user's NanoPi and the QEMU measurements below must not be treated as NanoPi performance figures.

## Implemented

- Import one native `.conf` containing exactly one interface and one peer.
- Accept Legacy profiles with `S1`/`S2` and AWG 2.0 profiles with `S1`-`S4`; reject partially specified `S3`/`S4` and unsupported AWG 3 parameters.
- Normalize bare interface IPv4/IPv6 addresses to `/32` and `/128`, matching exports produced by the Amnezia client.
- Reject `PreUp`, `PostUp`, `PreDown`, and `PostDown`. Imported DNS, table, and route ownership are not applied.
- Keep the private key in a `0600` file and omit it and the raw profile from status, diagnostics, command arguments, and logs.
- Verify the running kernel ABI before using the kernel module. Fast Lane never force-installs a module.
- Create a netifd-managed `fastlane_awg` interface and a dedicated route table. The VPN endpoint remains reachable through WAN.
- Add an Xray `freedom` outbound pinned to the AWG interface and route mark. Xray continues to own GeoIP, GeoSite, and direct/proxy decisions.
- Check the candidate through its own Xray probe inbound before switching the user route.
- Switch manually between direct/VLESS and AWG through the running Xray API.
- On AWG failure, use a healthy VLESS replacement when available; otherwise select the direct outbound. Return from direct mode only after two successful AWG checks separated by five seconds.
- Expose import, check, connect, disconnect, and delete actions in CLI and LuCI, with separate interface and internet-check states.

## Stand results

The full `TestOpenWrtAmneziaWGPrototype` scenario passed with non-zero AWG 2.0 `S1-S4` values.

The same full scenario also passed in Legacy mode with only `S1`/`S2` and a bare IPv4 interface address. The measured hot switch was 2.083 seconds, failure to managed direct was 15.204 seconds, recovery was 16.682 seconds, and the QEMU throughput sample was 8.61 Mbit/s. These remain stand measurements, not NanoPi performance figures.

- A real AWG handshake and HTTPS egress succeeded.
- TCP through Xray/AWG, UDP through the AWG route table, and DNS through the AWG route table succeeded.
- A domain configured as direct was observed on WAN and was not observed on the AWG interface.
- Turning the server interface off caused a managed direct fallback. Restoring it caused a confirmed return to AWG.
- Xray and dnsmasq PIDs remained unchanged during candidate checks, switching, failure, and recovery.
- Disconnect and delete removed the owned profile, netifd interface, and policy-routing rule.
- Across two successful final runs, the measured hot switch on QEMU ranged from 1.572 to 1.958 seconds.
- Confirmed AWG failure to managed direct fallback ranged from 15.166 to 16.864 seconds; confirmed recovery from direct back to AWG ranged from 15.855 to 21.670 seconds.
- The two QEMU samples reported Xray `VmRSS` from 35,812 to 35,972 KiB and AWG download throughput from 13.00 to 14.02 Mbit/s. These are stand diagnostics, not NanoPi capacity estimates.
- The complete OpenWrt/LuCI lifecycle passed separately, including installation, Russian translation, desktop/mobile UI, reboot restoration, disconnect, firewall removal, local DNS listener shutdown, and removal of stale DNS snippets across a dnsmasq restart.

## Profile supplied for validation

`R_RN2GTDRN_49.conf` and `R_RN2GTDRN_51.conf` were inspected without copying their secrets into the repository. The first is classified as Legacy because it has no `S3`/`S4`; the second is AWG 2.0 and uses a bare IPv4 interface address. Both now pass parser validation. Real egress still has to be confirmed on compatible OpenWrt hardware before claiming that either user profile connects successfully.

## Remaining limitations

- One AWG profile and one peer only.
- Manual AWG selection only; AWG does not yet join the reserve ranking or speed optimizer.
- No public release and no automatic module/package installation.
- Kernel package availability and ABI compatibility must be confirmed separately for the target NanoPi firmware.
- Existing TCP/UDP sessions may break when the failed tunnel disappears; only new traffic is moved to the replacement route.
- A target-device trial is still required to measure NanoPi CPU, memory, throughput, thermals, and real recovery time before release.
