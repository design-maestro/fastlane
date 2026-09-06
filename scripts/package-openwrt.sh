#!/bin/sh
set -eu

PKG_DIR="${PKG_DIR:-dist/fastlane-ipk}"
ARCH="${ARCH:-mipsel_24kc}"
ROOT_DIR="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
BINARY_PATH="${BINARY_PATH:-${ROOT_DIR}/bin/openwrt/fastlane}"
DATA_DIR="${PKG_DIR}/data"
CONTROL_DIR="${PKG_DIR}/control"
WORK_DIR="${PKG_DIR}/work"
PACKAGE_NAME="${PACKAGE_NAME:-fastlane}"

resolve_version() {
	if [ -n "${VERSION:-}" ]; then
		printf '%s\n' "${VERSION#v}"
		return
	fi

	if command -v git >/dev/null 2>&1; then
		version="$(git -C "${ROOT_DIR}" describe --tags --always --dirty 2>/dev/null || true)"
		if [ -n "${version}" ]; then
			printf '%s\n' "${version#v}"
			return
		fi
	fi

	printf '0.0.0-dev\n'
}

VERSION="$(resolve_version)"
IPK_PATH="${ROOT_DIR}/dist/${PACKAGE_NAME}_${VERSION}_${ARCH}.ipk"
TARBALL_PATH="${ROOT_DIR}/dist/${PACKAGE_NAME}_${VERSION}_${ARCH}.tar.gz"

rm -rf "${PKG_DIR}"
mkdir -p "${ROOT_DIR}/dist"
mkdir -p \
	"${DATA_DIR}" \
	"${DATA_DIR}/etc/uci-defaults" \
	"${DATA_DIR}/usr/bin" \
	"${DATA_DIR}/usr/lib/lua/luci/i18n" \
	"${DATA_DIR}/usr/share/licenses/fastlane" \
	"${DATA_DIR}/usr/share/luci/menu.d" \
	"${DATA_DIR}/usr/share/rpcd/acl.d" \
	"${DATA_DIR}/www/luci-static/resources/fastlane" \
	"${DATA_DIR}/www/luci-static/resources/fastlane/assets" \
	"${DATA_DIR}/www/luci-static/resources/view/fastlane" \
	"${CONTROL_DIR}" \
	"${WORK_DIR}"

cp "${BINARY_PATH}" "${DATA_DIR}/usr/bin/fastlane"
cp -R "${ROOT_DIR}/openwrt/root/." "${DATA_DIR}/"
cp "${ROOT_DIR}/scripts/uninstall.sh" "${DATA_DIR}/usr/libexec/fastlane-uninstall"
cp "${ROOT_DIR}/LICENSE" "${DATA_DIR}/usr/share/licenses/fastlane/LICENSE"
cp "${ROOT_DIR}/NOTICE" "${DATA_DIR}/usr/share/licenses/fastlane/NOTICE"
cp "${ROOT_DIR}/THIRD_PARTY_NOTICES.md" "${DATA_DIR}/usr/share/licenses/fastlane/THIRD_PARTY_NOTICES.md"
cp "${ROOT_DIR}/LICENSES/UPSTREAM-MIT.txt" "${DATA_DIR}/usr/share/licenses/fastlane/UPSTREAM-MIT.txt"
[ -d "${DATA_DIR}/etc/init.d" ] && find "${DATA_DIR}/etc/init.d" -type f -exec chmod 0755 {} \;
[ -d "${DATA_DIR}/usr/libexec" ] && find "${DATA_DIR}/usr/libexec" -type f -exec chmod 0755 {} \;
cp "${ROOT_DIR}/luci-app-fastlane/root/usr/share/luci/menu.d/luci-app-fastlane.json" \
	"${DATA_DIR}/usr/share/luci/menu.d/luci-app-fastlane.json"
cp "${ROOT_DIR}/luci-app-fastlane/root/usr/share/rpcd/acl.d/luci-app-fastlane.json" \
	"${DATA_DIR}/usr/share/rpcd/acl.d/luci-app-fastlane.json"

compile_translation() {
	po_file="${ROOT_DIR}/luci-app-fastlane/po/ru/fastlane.po"
	lmo_file="${DATA_DIR}/usr/lib/lua/luci/i18n/fastlane.ru.lmo"

	if [ -n "${PO2LMO:-}" ]; then
		"${PO2LMO}" "${po_file}" "${lmo_file}"
	elif command -v po2lmo >/dev/null 2>&1; then
		po2lmo "${po_file}" "${lmo_file}"
	elif command -v go >/dev/null 2>&1; then
		(
			cd "${ROOT_DIR}"
			go run ./cmd/po2lmo "${po_file}" "${lmo_file}"
		)
	else
		printf '%s\n' 'po2lmo or Go is required to compile the LuCI translation' >&2
		exit 1
	fi

	[ -s "${lmo_file}" ] || {
		printf '%s\n' 'compiled LuCI translation is empty' >&2
		exit 1
	}
}

compile_translation
cat > "${DATA_DIR}/etc/uci-defaults/luci-i18n-fastlane-ru" <<'EOF'
#!/bin/sh
uci -q set luci.languages.ru='Русский (Russian)' >/dev/null 2>&1 || true
uci -q commit luci >/dev/null 2>&1 || true
exit 0
EOF
chmod 0755 "${DATA_DIR}/etc/uci-defaults/luci-i18n-fastlane-ru"
cp "${ROOT_DIR}/luci-app-fastlane/htdocs/luci-static/resources/fastlane/"*.js \
	"${DATA_DIR}/www/luci-static/resources/fastlane/"
cp "${ROOT_DIR}/luci-app-fastlane/htdocs/luci-static/resources/fastlane/assets/"*.png \
	"${DATA_DIR}/www/luci-static/resources/fastlane/assets/"
for view_name in \
	vpn.js vpn-20260906-latency-v19.js \
	routing.js routing-20260906-v4.js \
	diagnostics.js diagnostics-20260904-v3.js \
	settings.js settings-20260905-updates-v6.js
do
	cp "${ROOT_DIR}/luci-app-fastlane/htdocs/luci-static/resources/view/fastlane/${view_name}" \
		"${DATA_DIR}/www/luci-static/resources/view/fastlane/${view_name}"
done

cat > "${CONTROL_DIR}/control" <<EOF
Package: ${PACKAGE_NAME}
Version: ${VERSION}
Architecture: ${ARCH}
Maintainer: design-maestro
License: PolyForm-Noncommercial-1.0.0
Section: net
Priority: optional
Depends: ca-bundle, nftables, kmod-nft-tproxy, rpcd-mod-file
Description: Fast Lane OpenWrt subscription proxy manager with LuCI frontend files
 This standalone IPK requires an existing /usr/bin/xray runtime; use install.sh for the bundled Xray installer.
EOF

cat > "${CONTROL_DIR}/postinst" <<'EOF'
#!/bin/sh
set -eu

harden_secret_storage() {
	if [ -d /etc/fastlane ]; then
		chmod 0700 /etc/fastlane >/dev/null 2>&1 || true
		for path in \
			/etc/fastlane/subscriptions.json \
			/etc/fastlane/settings.json \
			/etc/fastlane/state.json \
			/etc/fastlane/.fastlane.lock \
			/etc/fastlane/speedtest.lock
		do
			[ -e "${path}" ] && chmod 0600 "${path}" >/dev/null 2>&1 || true
		done
		find /etc/fastlane -maxdepth 1 -type f -name '*.corrupt-*' -exec chmod 0600 {} \; >/dev/null 2>&1 || true
	fi

	for path in /etc/xray/config.json /etc/xray/config.json.last-known-good; do
		[ -e "${path}" ] && chmod 0600 "${path}" >/dev/null 2>&1 || true
	done
}

register_languages() {
	language_defaults=/etc/uci-defaults/luci-i18n-fastlane-ru
	if [ -x "${language_defaults}" ]; then
		"${language_defaults}" >/dev/null 2>&1 || true
		rm -f "${language_defaults}"
	fi
}

if [ -n "${IPKG_INSTROOT:-}" ]; then
	exit 0
fi

if [ ! -x /usr/bin/xray ]; then
	printf '%s\n' 'fastlane: required Xray runtime /usr/bin/xray is missing; install Xray first or use the bundled install.sh' >&2
	exit 1
fi

chmod 0755 /etc/init.d/fastlane >/dev/null 2>&1 || true
chmod 0755 /usr/libexec/fastlane-cron >/dev/null 2>&1 || true
rm -f \
	/www/luci-static/resources/view/fastlane/logs.js \
	>/dev/null 2>&1 || true
harden_secret_storage
register_languages
/usr/libexec/fastlane-cron ensure-xray-log-retention >/dev/null 2>&1 || true
rm -f /tmp/luci-indexcache
rm -rf /tmp/luci-modulecache
/etc/init.d/rpcd reload >/dev/null 2>&1 || true
/etc/init.d/uhttpd reload >/dev/null 2>&1 || true

if [ -x /etc/init.d/fastlane ]; then
	if [ "${PKG_UPGRADE:-0}" = "1" ]; then
		if /etc/init.d/fastlane running >/dev/null 2>&1; then
			/etc/init.d/fastlane restart >/dev/null 2>&1 || true
		fi
	else
		/etc/init.d/fastlane enable >/dev/null 2>&1 || true
		/etc/init.d/fastlane start >/dev/null 2>&1 || true
	fi
fi
EOF
chmod 0755 "${CONTROL_DIR}/postinst"

printf '2.0\n' > "${WORK_DIR}/debian-binary"

create_tarball() {
	src_dir="$1"
	out_file="$2"
	out_file_dir="$(CDPATH= cd -- "$(dirname "${out_file}")" && pwd)"
	out_file="${out_file_dir}/$(basename "${out_file}")"

	(
		cd "${src_dir}"
		entries="$(find . -mindepth 1 -maxdepth 1 -print | LC_ALL=C sort)"
		if command -v bsdtar >/dev/null 2>&1; then
			# shellcheck disable=SC2086
			COPYFILE_DISABLE=1 bsdtar --format ustar --uid 0 --gid 0 --uname root --gname root -czf "${out_file}" ${entries}
			return
		fi

		if command -v tar >/dev/null 2>&1; then
			# shellcheck disable=SC2086
			COPYFILE_DISABLE=1 tar --format=ustar --owner=0 --group=0 --numeric-owner -czf "${out_file}" ${entries}
			return
		fi

		printf 'neither bsdtar nor tar is available\n' >&2
		exit 1
	)
}

create_tarball "${CONTROL_DIR}" "${WORK_DIR}/control.tar.gz"
create_tarball "${DATA_DIR}" "${WORK_DIR}/data.tar.gz"
create_tarball "${DATA_DIR}" "${TARBALL_PATH}"

rm -f "${IPK_PATH}"
printf '!<arch>\n' > "${IPK_PATH}"

write_ar_member() {
	name="$1"
	file="$2"
	size="$(wc -c < "${file}" | tr -d ' ')"
	timestamp="$(date +%s)"

	printf '%-16s%-12s%-6s%-6s%-8s%-10s`\n' \
		"${name}/" \
		"${timestamp}" \
		"0" \
		"0" \
		"100644" \
		"${size}" >> "${IPK_PATH}"
	cat "${file}" >> "${IPK_PATH}"

	if [ $((size % 2)) -ne 0 ]; then
		printf '\n' >> "${IPK_PATH}"
	fi
}

write_ar_member "debian-binary" "${WORK_DIR}/debian-binary"
write_ar_member "control.tar.gz" "${WORK_DIR}/control.tar.gz"
write_ar_member "data.tar.gz" "${WORK_DIR}/data.tar.gz"

write_checksum() {
	file="$1"
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "${file}" | awk -v name="$(basename "${file}")" '{ print $1 "  " name }' > "${file}.sha256"
		return
	fi
	shasum -a 256 "${file}" | awk -v name="$(basename "${file}")" '{ print $1 "  " name }' > "${file}.sha256"
}

write_checksum "${IPK_PATH}"
write_checksum "${TARBALL_PATH}"

echo "Created ${IPK_PATH}"
echo "Created ${TARBALL_PATH}"
echo "Created ${IPK_PATH}.sha256"
echo "Created ${TARBALL_PATH}.sha256"
