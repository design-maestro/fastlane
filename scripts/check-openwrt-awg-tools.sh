#!/bin/sh
set -eu

DIST_DIR="${1:-dist}"
found=0

for archive in "${DIST_DIR}"/fastlane_*.tar.gz; do
	[ -f "${archive}" ] || continue
	found=1
	tmp_dir="$(mktemp -d)"
	trap 'rm -rf "${tmp_dir}"' EXIT INT TERM
	tar -xzf "${archive}" -C "${tmp_dir}"
	tool="${tmp_dir}/usr/libexec/fastlane-amneziawg"
	[ -x "${tool}" ] || {
		printf 'missing packaged Fast Lane AmneziaWG tool in %s\n' "${archive}" >&2
		exit 1
	}
	if readelf -l "${tool}" | grep -q 'Requesting program interpreter'; then
		printf 'Fast Lane AmneziaWG tool must be statically linked: %s\n' "${archive}" >&2
		exit 1
	fi
	rm -rf "${tmp_dir}"
	trap - EXIT INT TERM
done

[ "${found}" -eq 1 ] || {
	printf 'no Fast Lane release archives found in %s\n' "${DIST_DIR}" >&2
	exit 1
}
