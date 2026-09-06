#!/bin/sh
set -eu

# Path-only guard. It deliberately never reads file contents.
tracked_paths="$(git ls-files)"
staged_paths="$(git diff --cached --name-only --diff-filter=ACMR 2>/dev/null || true)"
candidate_paths="${tracked_paths}
${staged_paths}"

violations="$(printf '%s\n' "${candidate_paths}" | awk '
	NF && !seen[$0]++ {
		path = $0
		if (path ~ /^backups\// ||
		    path ~ /(^|\/)etc\/shadow$/ ||
		    path ~ /(^|\/)etc\/dropbear\/.*host_key$/ ||
		    path ~ /(^|\/)etc\/uhttpd\.key$/ ||
		    path ~ /(^|\/)etc\/samba\/secrets\.tdb$/ ||
		    path ~ /(^|\/)etc\/nikki\//) {
			print path
		}
	}
')"

if [ -n "${violations}" ]; then
	printf '%s\n' 'Refusing to continue: sensitive router paths are tracked or staged:' >&2
	printf '%s\n' "${violations}" >&2
	exit 1
fi

printf '%s\n' 'Sensitive path check passed.'
