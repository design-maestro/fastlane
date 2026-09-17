#!/bin/sh
set -eu

ROOT_DIR="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
OUTPUT_DIR="${OUTPUT_DIR:-${ROOT_DIR}/bin/openwrt}"
AWG_CC="${AWG_CC:-cc}"
AWG_TOOLS_COMMIT="ee0f0a9aa34ff0a0da4b3433b9512781cfe02843"
AWG_TOOLS_SHA256="22438f231d39ea27e4bdc69707ec3103357f3cecc9ad3eecba18a61e5b39c9b9"
AWG_TOOLS_VERSION="3.1.20260812"
build_cflags="-O2 -static -D_GNU_SOURCE -D'WIREGUARD_TOOLS_VERSION=\"${AWG_TOOLS_VERSION}\"'"
if [ "$(uname -s)" = "Darwin" ]; then
	build_cflags="-O2 -D_GNU_SOURCE -D'WIREGUARD_TOOLS_VERSION=\"${AWG_TOOLS_VERSION}\"'"
fi

mkdir -p "${OUTPUT_DIR}"
OUTPUT_DIR="$(CDPATH= cd -- "${OUTPUT_DIR}" && pwd)"
work_dir="$(mktemp -d)"
trap 'rm -rf "${work_dir}"' EXIT INT TERM
archive="${work_dir}/amneziawg-tools.tar.gz"

curl --fail --location --silent --show-error \
	"https://github.com/amnezia-vpn/amneziawg-tools/archive/${AWG_TOOLS_COMMIT}.tar.gz" \
	-o "${archive}"
actual_sum="$(shasum -a 256 "${archive}" | awk '{print $1}')"
[ "${actual_sum}" = "${AWG_TOOLS_SHA256}" ] || {
	printf 'unexpected checksum for amneziawg-tools: %s\n' "${actual_sum}" >&2
	exit 1
}

tar -xzf "${archive}" -C "${work_dir}"
source_dir="${work_dir}/amneziawg-tools-${AWG_TOOLS_COMMIT}/src"
[ -f "${source_dir}/Makefile" ] || {
	printf '%s\n' 'amneziawg-tools source layout is unexpected' >&2
	exit 1
}

(
	cd "${source_dir}"
	CC="${AWG_CC}" PLATFORM=linux CFLAGS="${build_cflags}" make
)
install -m 0755 "${source_dir}/wg" "${OUTPUT_DIR}/amneziawg"
install -m 0644 "${work_dir}/amneziawg-tools-${AWG_TOOLS_COMMIT}/COPYING" "${OUTPUT_DIR}/amneziawg-tools-COPYING"
