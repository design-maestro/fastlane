#!/bin/sh
set -eu

ROOT_DIR="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
OUTPUT_DIR="${OUTPUT_DIR:-${ROOT_DIR}/bin/openwrt}"
GOOS="${GOOS:-linux}"
GOARCH="${GOARCH:-arm64}"
GOMIPS="${GOMIPS:-}"
AWG_GO_MODULE="github.com/amnezia-vpn/amneziawg-go/v3"
AWG_GO_VERSION="v3.1.20260828"
AWG_GO_SUM="h1:D8d8gGvwXcTxUIsE4z6F6vjy4/VZddu95vMNtOygh1c="

mkdir -p "${OUTPUT_DIR}"
OUTPUT_DIR="$(CDPATH= cd -- "${OUTPUT_DIR}" && pwd)"
module_json="$(go mod download -json "${AWG_GO_MODULE}@${AWG_GO_VERSION}")"
module_dir="$(printf '%s\n' "${module_json}" | sed -n 's/^[[:space:]]*"Dir": "\(.*\)",$/\1/p')"
module_sum="$(printf '%s\n' "${module_json}" | sed -n 's/^[[:space:]]*"Sum": "\(.*\)",$/\1/p')"
[ -n "${module_dir}" ] || {
	printf 'cannot locate downloaded %s@%s\n' "${AWG_GO_MODULE}" "${AWG_GO_VERSION}" >&2
	exit 1
}
[ "${module_sum}" = "${AWG_GO_SUM}" ] || {
	printf 'unexpected checksum for %s@%s: %s\n' "${AWG_GO_MODULE}" "${AWG_GO_VERSION}" "${module_sum}" >&2
	exit 1
}

(
	cd "${module_dir}"
	CGO_ENABLED=0 GOOS="${GOOS}" GOARCH="${GOARCH}" GOMIPS="${GOMIPS}" \
		go build -trimpath -ldflags='-s -w' -o "${OUTPUT_DIR}/amneziawg-go" .
)
chmod 0755 "${OUTPUT_DIR}/amneziawg-go"
