#!/bin/sh
# Fast Lane-owned AmneziaWG netifd protocol. Supports amneziawg-go v3.1.
# Based on the upstream amneziawg-tools protocol handler (Apache-2.0).

SYSTEM_AWG=/usr/bin/awg
FASTLANE_AWG=/usr/libexec/fastlane-amneziawg
[ -n "$INCLUDE_ONLY" ] || { . /lib/functions.sh; . ../netifd-proto.sh; init_proto "$@"; }

proto_amneziawg_init_config() {
	proto_config_add_string private_key
	proto_config_add_int mtu
	proto_config_add_int listen_port
	proto_config_add_string fwmark
	proto_config_add_string awg_jc; proto_config_add_string awg_jmin; proto_config_add_string awg_jmax
	proto_config_add_string awg_s1; proto_config_add_string awg_s2; proto_config_add_string awg_s3; proto_config_add_string awg_s4
	proto_config_add_string awg_h1; proto_config_add_string awg_h2; proto_config_add_string awg_h3; proto_config_add_string awg_h4
	proto_config_add_string awg_i1; proto_config_add_string awg_i2; proto_config_add_string awg_i3; proto_config_add_string awg_i4; proto_config_add_string awg_i5
	proto_config_add_string awg_header_protection_key
	proto_config_add_string awg_content_padding_addition
	proto_config_add_string awg_rekey_after_time
	proto_config_add_string awg_rekey_timeout
	proto_config_add_string awg_reject_after_time
	proto_config_add_string awg_keepalive_timeout
	proto_config_add_string awg_max_handshake_attempts
	proto_config_add_boolean awg_random_trailers
	proto_config_add_boolean awg_disable_cookies
	proto_config_add_boolean awg_force_userspace
	available=1; no_proto_task=1
}

proto_amneziawg_kernel() {
	[ -e /sys/module/amneziawg ]
}

proto_amneziawg_peer() {
	local peer="$1" disabled public_key preshared_key allowed_ips endpoint_host endpoint_port persistent_keepalive
	config_get_bool disabled "$peer" disabled 0; [ "$disabled" = 1 ] && return
	config_get public_key "$peer" public_key; [ -z "$public_key" ] && return
	config_get preshared_key "$peer" preshared_key; config_get allowed_ips "$peer" allowed_ips
	config_get endpoint_host "$peer" endpoint_host; config_get endpoint_port "$peer" endpoint_port
	config_get persistent_keepalive "$peer" persistent_keepalive
	echo '[Peer]' >> "$awg_cfg"; echo "PublicKey=$public_key" >> "$awg_cfg"
	[ -n "$preshared_key" ] && echo "PresharedKey=$preshared_key" >> "$awg_cfg"
	for ip in $allowed_ips; do echo "AllowedIPs=$ip" >> "$awg_cfg"; done
	if [ -n "$endpoint_host" ]; then case "$endpoint_host" in *:*) endpoint_host="[$endpoint_host]";; esac; echo "Endpoint=$endpoint_host:${endpoint_port:-51820}" >> "$awg_cfg"; fi
	[ -n "$persistent_keepalive" ] && echo "PersistentKeepalive=$persistent_keepalive" >> "$awg_cfg"
}

proto_amneziawg_emit() { local value; config_get value "$config" "$1"; [ -n "$value" ] && echo "$2=$value" >> "$awg_cfg"; }
proto_amneziawg_emit_bool() {
	local value
	config_get value "$config" "$1"
	[ -n "$value" ] || return 0
	case "$value" in
		1|true|on) value=on ;;
		*) value=off ;;
	esac
	echo "$2=$value" >> "$awg_cfg"
}

proto_amneziawg_setup() {
	local config="$1" private_key addresses mtu force_userspace result awg_cfg="" awg_err="" attempt ready=0
	local awg_tool nohostroute tunlink
	config_load network
	config_get private_key "$config" private_key
	config_get addresses "$config" addresses
	config_get mtu "$config" mtu
	config_get nohostroute "$config" nohostroute
	config_get tunlink "$config" tunlink
	config_get_bool force_userspace "$config" awg_force_userspace 0
	# Keep Legacy and AWG 2.0 on the router's established toolchain. The
	# bundled 3.1 tool is selected only for profiles that need extended fields.
	if [ "$force_userspace" = 1 ]; then
		awg_tool="$FASTLANE_AWG"
	elif [ -x "$SYSTEM_AWG" ]; then
		awg_tool="$SYSTEM_AWG"
	else
		awg_tool="$FASTLANE_AWG"
	fi
	[ -x "$awg_tool" ] || { proto_setup_failed "$config"; return 1; }
	proto_amneziawg_fail() {
		logger -t fastlane-awg "interface $config configuration rejected"
		rm -f "$awg_cfg" "$awg_err"
		ip link del dev "$config" 2>/dev/null || true
		rm -f "/var/run/amneziawg/$config.sock"
		proto_block_restart "$config"
		proto_setup_failed "$config"
		return 1
	}
	[ -n "$private_key" ] || { proto_amneziawg_fail; return 1; }
	if [ "$force_userspace" = 1 ]; then
		command -v amneziawg-go >/dev/null || { proto_amneziawg_fail; return 1; }
		ip link del dev "$config" 2>/dev/null || true
		rm -f "/var/run/amneziawg/$config.sock"
		amneziawg-go "$config" >/dev/null 2>&1 || { proto_amneziawg_fail; return 1; }
		# The userspace daemon forks before its UAPI socket is ready. Waiting
		# here prevents awg from falling back to the older kernel path and
		# rejecting AWG 3.1 fields during the short startup race.
		for attempt in 1 2 3 4 5; do
			[ -S "/var/run/amneziawg/$config.sock" ] && { ready=1; break; }
			sleep 1
		done
		[ "$ready" = 1 ] || { proto_amneziawg_fail; return 1; }
	elif proto_amneziawg_kernel; then
		ip link del dev "$config" 2>/dev/null
		ip link add dev "$config" type amneziawg || { proto_amneziawg_fail; return 1; }
	elif command -v amneziawg-go >/dev/null; then rm -f "/var/run/amneziawg/$config.sock"; amneziawg-go "$config" >/dev/null 2>&1 || { proto_amneziawg_fail; return 1; }
	else proto_amneziawg_fail; return 1; fi
	proto_init_update "$config" 1
	umask 077; mkdir -p /tmp/amneziawg; awg_cfg="/tmp/amneziawg/$config"; awg_err="${awg_cfg}.err"
	echo '[Interface]' > "$awg_cfg"; echo "PrivateKey=$private_key" >> "$awg_cfg"
	proto_amneziawg_emit listen_port ListenPort; proto_amneziawg_emit fwmark FwMark
	proto_amneziawg_emit awg_jc Jc; proto_amneziawg_emit awg_jmin Jmin; proto_amneziawg_emit awg_jmax Jmax
	proto_amneziawg_emit awg_s1 S1; proto_amneziawg_emit awg_s2 S2; proto_amneziawg_emit awg_s3 S3; proto_amneziawg_emit awg_s4 S4
	proto_amneziawg_emit awg_h1 H1; proto_amneziawg_emit awg_h2 H2; proto_amneziawg_emit awg_h3 H3; proto_amneziawg_emit awg_h4 H4
	proto_amneziawg_emit awg_i1 I1; proto_amneziawg_emit awg_i2 I2; proto_amneziawg_emit awg_i3 I3; proto_amneziawg_emit awg_i4 I4; proto_amneziawg_emit awg_i5 I5
	proto_amneziawg_emit awg_header_protection_key HeaderProtectionKey
	proto_amneziawg_emit awg_content_padding_addition ContentPaddingAddition
	proto_amneziawg_emit awg_rekey_after_time RekeyAfterTime; proto_amneziawg_emit awg_rekey_timeout RekeyTimeout
	proto_amneziawg_emit awg_reject_after_time RejectAfterTime; proto_amneziawg_emit awg_keepalive_timeout KeepaliveTimeout
	proto_amneziawg_emit awg_max_handshake_attempts MaxHandshakeAttempts
	proto_amneziawg_emit_bool awg_random_trailers RandomTrailers; proto_amneziawg_emit_bool awg_disable_cookies DisableCookies
	config_foreach proto_amneziawg_peer "amneziawg_$config"
	"$awg_tool" setconf "$config" "$awg_cfg" >/dev/null 2>"$awg_err"; result=$?; rm -f "$awg_cfg" "$awg_err"
	[ "$result" -eq 0 ] || { proto_amneziawg_fail; return 1; }
	# Applying the UAPI configuration can restore the userspace device's
	# default MTU. Set the profile MTU only after setconf so the value from the
	# imported profile is the one left on the live interface.
	[ -n "$mtu" ] && ip link set mtu "$mtu" dev "$config"
	for address in $addresses; do case "$address" in *:*/*) proto_add_ipv6_address "${address%%/*}" "${address##*/}";; *.*/*) proto_add_ipv4_address "${address%%/*}" "${address##*/}";; esac; done
	# Keep the encrypted endpoint outside the tunnel. Without this upstream
	# netifd dependency, a later route reload can capture the outer UDP path.
	if [ "$nohostroute" != 1 ]; then
		"$awg_tool" show "$config" endpoints 2>/dev/null | \
			sed -E 's/\[?([0-9.:a-f]+)\]?:([0-9]+)/\1 \2/' | \
			while read -r key address port; do
				[ -n "$port" ] || continue
				proto_add_host_dependency "$config" "$address" "$tunlink"
			done
	fi
	proto_send_update "$config"
	# netifd applies the L3 update synchronously here. Reassert the requested
	# MTU afterwards as well so a device-default update cannot silently return
	# userspace AWG to 1420.
	[ -n "$mtu" ] && ip link set mtu "$mtu" dev "$config"
}

proto_amneziawg_teardown() {
	local config="$1"
	# Deleting TUN closes the userspace device too; removing just the socket
	# leaves an orphan daemon/interface and breaks the next preparation.
	ip link del dev "$config" >/dev/null 2>&1 || true
	rm -f "/var/run/amneziawg/$config.sock"
}
[ -n "$INCLUDE_ONLY" ] || add_protocol amneziawg
