# Security policy

## Reporting a vulnerability

Please do not open a public issue for a vulnerability, leaked credential, routing
bypass, privilege escalation, or installer/uninstaller safety problem.

Use GitHub's **Security → Report a vulnerability** flow for this repository. Add:

- affected version or commit;
- device and OpenWrt/FriendlyWrt version;
- the smallest reproducible steps;
- expected and observed behavior;
- impact and any known workaround.

Remove subscription URLs, UUIDs, private keys, cookies, public IPs, and LAN device
details from the report unless they are strictly required. Never attach a full
router backup.

## Scope

Security-sensitive areas include the root-running service, installer and
uninstaller, LuCI RPC permissions, generated Xray configuration, nftables and DNS
changes, update downloads, secret-bearing state, and recovery behavior.

Supported fixes target the current `main` branch and the latest current Fast Lane
release. Older development snapshots may require an upgrade before a fix can be
applied.
