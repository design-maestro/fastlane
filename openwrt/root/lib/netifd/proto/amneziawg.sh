#!/bin/sh
# Fast Lane-owned AmneziaWG netifd protocol. Supports amneziawg-go v3.1.
# Based on the upstream amneziawg-tools protocol handler (Apache-2.0).

AWG=/usr/bin/awg
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
	available=1; no_proto_task=1
}

proto_amneziawg_kernel() {
	[ -e /sys/module/amneziawg ] || modprobe amneziawg >/dev/null 2>&1 || true
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

proto_amneziawg_setup() {
	local config="$1" private_key addresses mtu
	config_load network
	config_get private_key "$config" private_key
	config_get addresses "$config" addresses
	config_get mtu "$config" mtu
	[ -n "$private_key" ] || { proto_setup_failed "$config"; return 1; }
	if proto_amneziawg_kernel; then ip link del dev "$config" 2>/dev/null; ip link add dev "$config" type amneziawg
	elif command -v amneziawg-go >/dev/null; then rm -f "/var/run/amneziawg/$config.sock"; amneziawg-go "$config"
	else proto_setup_failed "$config"; return 1; fi
	[ -n "$mtu" ] && ip link set mtu "$mtu" dev "$config"
	proto_init_update "$config" 1
	umask 077; mkdir -p /tmp/amneziawg; awg_cfg="/tmp/amneziawg/$config"
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
	proto_amneziawg_emit awg_random_trailers RandomTrailers; proto_amneziawg_emit awg_disable_cookies DisableCookies
	config_foreach proto_amneziawg_peer "amneziawg_$config"
	"$AWG" setconf "$config" "$awg_cfg"; result=$?; rm -f "$awg_cfg"
	[ "$result" -eq 0 ] || { proto_setup_failed "$config"; return 1; }
	for address in $addresses; do case "$address" in *:*/*) proto_add_ipv6_address "${address%%/*}" "${address##*/}";; *.*/*) proto_add_ipv4_address "${address%%/*}" "${address##*/}";; esac; done
	proto_send_update "$config"
}

proto_amneziawg_teardown() { local config="$1"; if proto_amneziawg_kernel; then ip link del dev "$config" >/dev/null 2>&1; else rm -f "/var/run/amneziawg/$config.sock"; fi; }
[ -n "$INCLUDE_ONLY" ] || add_protocol amneziawg
