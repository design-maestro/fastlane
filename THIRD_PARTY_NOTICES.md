# Third-party notices

## Earlier MIT-licensed codebase

Some portions of Fast Lane are derived from an earlier MIT-licensed codebase.
The original copyright and license notice are preserved in
[LICENSES/UPSTREAM-MIT.txt](LICENSES/UPSTREAM-MIT.txt). Those MIT terms apply to
the inherited portions; Fast Lane's original contributions are licensed under
the top-level [PolyForm Noncommercial License 1.0.0](LICENSE).

## LuCI LMO compiler compatibility

`cmd/po2lmo` implements the LMO archive format and SuperFastHash behavior used
by OpenWrt LuCI's `po2lmo` tool. The compatible reference implementation is:

- Copyright (C) 2009-2012 Jo-Philipp Wich
- Licensed under the Apache License, Version 2.0
- Source: <https://github.com/openwrt/luci/tree/master/modules/luci-base/src>
- License: <https://www.apache.org/licenses/LICENSE-2.0>

The command is a build-time tool and is not installed on the router.
