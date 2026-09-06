#!/bin/sh
set -eu

FASTLANE_INSTALL_ROOT="${FASTLANE_INSTALL_ROOT:-/}"
FASTLANE_ROOT_OVERRIDE="${FASTLANE_ROOT:-}"
FASTLANE_XRAY_BINARY_PATH="${FASTLANE_XRAY_BINARY:-}"
FASTLANE_XRAY_SERVICE_PATH="${FASTLANE_XRAY_SERVICE:-}"
FASTLANE_XRAY_CONFIG_PATH="${FASTLANE_XRAY_CONFIG:-}"
FASTLANE_DRY_RUN=0
FASTLANE_CONFIRMED=0

usage() {
	printf '%s\n' "Fast Lane OpenWrt uninstaller"
	printf '%s\n' ""
	printf '%s\n' "Usage: uninstall.sh [--install-root <path>] [--dry-run] [--confirm]"
}

log() {
	printf '%s\n' "$*"
}

die() {
	printf 'error: %s\n' "$*" >&2
	exit 1
}

scope_path() {
	path="$1"

	case "${path}" in
		/*)
			if [ "${FASTLANE_INSTALL_ROOT}" = "/" ]; then
				printf '%s\n' "${path}"
			else
				printf '%s%s\n' "${FASTLANE_INSTALL_ROOT%/}" "${path}"
			fi
			;;
		*)
			if [ "${FASTLANE_INSTALL_ROOT}" = "/" ]; then
				printf '%s\n' "${path}"
			else
				printf '%s/%s\n' "${FASTLANE_INSTALL_ROOT%/}" "${path}"
			fi
			;;
	esac
}

fastlane_root_path() {
	path="${FASTLANE_ROOT_OVERRIDE:-/etc/fastlane}"
	scope_path "${path}"
}

fastlane_binary_path() {
	scope_path "/usr/bin/fastlane"
}

fastlane_service_path() {
	scope_path "/etc/init.d/fastlane"
}

xray_binary_path() {
	path="${FASTLANE_XRAY_BINARY_PATH:-/usr/bin/xray}"
	scope_path "${path}"
}

xray_service_path() {
	path="${FASTLANE_XRAY_SERVICE_PATH:-/etc/init.d/xray}"
	scope_path "${path}"
}

xray_config_path() {
	path="${FASTLANE_XRAY_CONFIG_PATH:-/etc/xray/config.json}"
	scope_path "${path}"
}

fastlane_cron_helper_path() {
	scope_path "/usr/libexec/fastlane-cron"
}

fastlane_self_update_helper_path() {
	scope_path "/usr/libexec/fastlane-self-update"
}

fastlane_xray_update_helper_path() {
	scope_path "/usr/libexec/fastlane-xray-update"
}

fastlane_uninstall_helper_path() {
	scope_path "/usr/libexec/fastlane-uninstall"
}

install_manifest_path() {
	printf '%s/install-manifest.txt\n' "$(fastlane_root_path)"
}

require_root_if_needed() {
	if [ "${FASTLANE_INSTALL_ROOT}" = "/" ] && [ "$(id -u)" -ne 0 ]; then
		die "run this uninstaller as root on the router, or use --install-root for a staging directory"
	fi
}

run_service_if_present() {
	script="$1"
	action="$2"

	[ -x "${script}" ] || return 0
	"${script}" "${action}" >/dev/null 2>&1 || return 1
}

remove_path() {
	path="$1"

	if [ -e "${path}" ] || [ -L "${path}" ]; then
		log "Removing ${path}"
		rm -rf "${path}"
	fi
}

remove_glob() {
	pattern="$1"

	for path in ${pattern}; do
		if [ -e "${path}" ] || [ -L "${path}" ]; then
			log "Removing ${path}"
			rm -rf "${path}"
		fi
	done
}

command_exists() {
	command -v "$1" >/dev/null 2>&1
}

opkg_available() {
	command_exists opkg
}

manifest_values() {
	kind="$1"
	manifest="$2"

	[ -f "${manifest}" ] || return 0
	awk -F= -v kind="${kind}" '$1 == kind { print $2 }' "${manifest}"
}

manifest_has_matching_pkg() {
	prefix="$1"
	manifest="$2"

	[ -f "${manifest}" ] || return 1
	grep -Eq "^pkg=${prefix}($|[-_])" "${manifest}"
}

manifest_has_runtime() {
	runtime="$1"
	manifest="$2"
	[ -f "${manifest}" ] || return 1
	grep -Fqx "runtime=${runtime}" "${manifest}"
}

remove_manifest_packages() {
	manifest="$1"

	if [ ! -f "${manifest}" ] || ! opkg_available; then
		return 0
	fi

	manifest_values "pkg" "${manifest}" | awk '{ lines[NR] = $0 } END { for (i = NR; i >= 1; i--) print lines[i] }' | while IFS= read -r pkg; do
		[ -n "${pkg}" ] || continue
		case "${pkg}" in zapret|zapret-*|zapret_*) log "Preserving external Zapret package ${pkg}"; continue ;; esac
		log "Removing installer-managed package ${pkg}"
		opkg remove "${pkg}" || true
	done
}

remove_opkg_package_if_installed() {
	pkg="$1"

	opkg_available || return 0
	if opkg list-installed "${pkg}" 2>/dev/null | grep -Fq "${pkg} -"; then
		log "Removing legacy installer-managed package ${pkg}"
		opkg remove "${pkg}" || true
	fi
}

restore_manifest_packages() {
	manifest="$1"

	if [ ! -f "${manifest}" ] || ! opkg_available; then
		return 0
	fi

	manifest_values "restore" "${manifest}" | while IFS= read -r pkg; do
		[ -n "${pkg}" ] || continue
		log "Restoring package ${pkg}"
		opkg install "${pkg}" || true
	done
}

strip_fastlane_opkg_feed_entries() {
	for config in "$(scope_path "/etc/opkg/customfeeds.conf")" $(scope_path "/etc/opkg/"*.conf); do
		if [ ! -f "${config}" ]; then
			continue
		fi

		tmp="${config}.fastlane-tmp.$$"
		if grep -vi "fastlane" "${config}" > "${tmp}" 2>/dev/null; then
			mv "${tmp}" "${config}"
		else
			: > "${config}"
			rm -f "${tmp}"
		fi
	done
}

remove_fastlane_opkg_keys() {
	for key in $(scope_path "/etc/opkg/keys/"*); do
		if [ ! -f "${key}" ]; then
			continue
		fi

		if grep -Fq "Fast Lane opkg feed" "${key}" 2>/dev/null; then
			remove_path "${key}"
		fi
	done
}

legacy_fastlane_install_detected() {
	if [ -e "${fastlane_self_update_helper}" ] || [ -e "${fastlane_xray_update_helper}" ]; then
		return 0
	fi

	if [ -d "${fastlane_root}" ] || [ -e "${fastlane_binary}" ] || [ -e "${fastlane_service}" ]; then
		return 0
	fi

	if [ -e "$(scope_path "/etc/sysctl.d/99-fastlane-ipv6.conf")" ]; then
		return 0
	fi

	for path in $(scope_path "/etc/init.d/fastlane.bak."*); do
		if [ -e "${path}" ] || [ -L "${path}" ]; then
			return 0
		fi
	done

	return 1
}

while [ "$#" -gt 0 ]; do
	case "$1" in
		--install-root)
			[ "$#" -ge 2 ] || die "missing value for --install-root"
			FASTLANE_INSTALL_ROOT="$2"
			shift 2
			;;
		--dry-run)
			FASTLANE_DRY_RUN=1
			shift
			;;
		--confirm)
			FASTLANE_CONFIRMED=1
			shift
			;;
		-h|--help)
			usage
			exit 0
			;;
		*)
			die "unknown argument: $1"
			;;
	esac
done

require_root_if_needed

if [ "${FASTLANE_INSTALL_ROOT}" = "/" ] && [ "${FASTLANE_DRY_RUN}" != "1" ] && [ "${FASTLANE_CONFIRMED}" != "1" ]; then
	die "refusing to uninstall Fast Lane without --confirm"
fi

fastlane_root="$(fastlane_root_path)"
fastlane_binary="$(fastlane_binary_path)"
fastlane_service="$(fastlane_service_path)"
xray_binary="$(xray_binary_path)"
xray_service="$(xray_service_path)"
xray_config="$(xray_config_path)"
xray_config_dir="$(dirname "${xray_config}")"
install_manifest="$(install_manifest_path)"
fastlane_cron_helper="$(fastlane_cron_helper_path)"
fastlane_self_update_helper="$(fastlane_self_update_helper_path)"
fastlane_xray_update_helper="$(fastlane_xray_update_helper_path)"
fastlane_uninstall_helper="$(fastlane_uninstall_helper_path)"
rpcd_service="$(scope_path "/etc/init.d/rpcd")"
uhttpd_service="$(scope_path "/etc/init.d/uhttpd")"
remove_owned_xray=0

if [ "${FASTLANE_DRY_RUN}" = "1" ]; then
	log "Fast Lane root: ${fastlane_root}"
	log "Fast Lane binary: ${fastlane_binary}"
	log "Fast Lane service: ${fastlane_service}"
	log "Xray binary: ${xray_binary}"
	log "Xray service: ${xray_service}"
	log "Xray config: ${xray_config}"
	log "Install manifest: ${install_manifest}"
	exit 0
fi

if manifest_has_runtime "xray" "${install_manifest}"; then
	remove_owned_xray=1
fi

if [ -x "${fastlane_binary}" ]; then
	"${fastlane_binary}" --root "${fastlane_root}" disconnect >/dev/null 2>&1 || true
	"${fastlane_binary}" --root "${fastlane_root}" firewall disable >/dev/null 2>&1 || true
fi

run_service_if_present "${fastlane_service}" stop || true
run_service_if_present "${fastlane_service}" disable || true
if [ "${remove_owned_xray}" = "1" ]; then
	run_service_if_present "${xray_service}" stop || true
	run_service_if_present "${xray_service}" disable || true
fi
FASTLANE_INSTALL_ROOT="${FASTLANE_INSTALL_ROOT}" "${fastlane_cron_helper}" remove-xray-log-retention >/dev/null 2>&1 || true
remove_manifest_packages "${install_manifest}"
restore_manifest_packages "${install_manifest}"

remove_path "${fastlane_binary}"
remove_path "${fastlane_service}"
remove_path "${fastlane_root}"

remove_path "$(scope_path "/usr/share/luci/menu.d/luci-app-fastlane.json")"
remove_path "$(scope_path "/usr/share/rpcd/acl.d/luci-app-fastlane.json")"
remove_path "$(scope_path "/www/luci-static/resources/fastlane")"
remove_path "$(scope_path "/www/luci-static/resources/view/fastlane")"

if [ "${remove_owned_xray}" = "1" ]; then
	remove_path "${xray_binary}"
	remove_path "${xray_service}"
	remove_path "${xray_config_dir}"
fi
remove_path "${fastlane_cron_helper}"
remove_path "${fastlane_self_update_helper}"
remove_path "${fastlane_xray_update_helper}"
remove_path "${fastlane_uninstall_helper}"
if [ "${remove_owned_xray}" = "1" ]; then
	remove_path "$(scope_path "/var/log/xray.log")"
	remove_path "$(scope_path "/var/run/xray.pid")"
fi
remove_path "$(scope_path "/etc/sysctl.d/99-fastlane-ipv6.conf")"

remove_glob "$(scope_path "/etc/rc.d/*fastlane")"
if [ "${remove_owned_xray}" = "1" ]; then
	remove_glob "$(scope_path "/etc/rc.d/*xray")"
fi
remove_glob "$(scope_path "/etc/init.d/fastlane.bak.*")"
remove_glob "$(scope_path "/tmp/fastlane*")"
if [ "${remove_owned_xray}" = "1" ]; then
	remove_glob "$(scope_path "/tmp/xray*")"
fi
remove_glob "$(scope_path "/tmp/lock/procd_fastlane*")"
remove_glob "$(scope_path "/tmp/luci-indexcache*")"
remove_path "$(scope_path "/tmp/luci-modulecache")"
strip_fastlane_opkg_feed_entries
remove_fastlane_opkg_keys

run_service_if_present "${rpcd_service}" reload || true
run_service_if_present "${uhttpd_service}" reload || true

log "Fast Lane and installer-managed dependencies removed; external Xray and Zapret were preserved."
