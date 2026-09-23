#!/bin/sh
set -eu

profile=${1:?usage: check-coverage.sh COVERAGE_PROFILE}
minimum=${COVERAGE_MIN:-90}
module=github.com/miguins/open-media-downloader-ios
# Security-critical packages must be fully covered.
full_packages=${COVERAGE_FULL_PACKAGES:-internal/auth internal/storage internal/urlpolicy}
coverage=$(go tool cover -func="$profile" | awk '$1 == "total:" { gsub(/%/, "", $3); print $3 }')

if [ -z "$coverage" ]; then
	echo "unable to read total unit coverage" >&2
	exit 1
fi

if ! awk -v coverage="$coverage" -v minimum="$minimum" 'BEGIN { exit !(coverage + 0 >= minimum + 0) }'; then
	echo "unit coverage: ${coverage}% (minimum: ${minimum}%)" >&2
	exit 1
fi

echo "unit coverage: ${coverage}% (minimum: ${minimum}%)"

for package in $full_packages; do
	# Blocks can repeat across test binaries; a block counts as covered if any run executed it.
	result=$(awk -v prefix="$module/$package/" '
		NR > 1 && index($1, prefix) == 1 && index(substr($1, length(prefix) + 1), "/") == 0 {
			statements[$1] = $2
			if ($3 > 0) covered[$1] = 1
		}
		END {
			for (block in statements) {
				total += statements[block]
				if (block in covered) hit += statements[block]
			}
			printf "%d %d", hit, total
		}' "$profile")
	hit=${result% *}
	total=${result#* }
	if [ "$total" -eq 0 ] || [ "$hit" -ne "$total" ]; then
		echo "$package coverage: $hit/$total statements (required: 100%)" >&2
		exit 1
	fi
	echo "$package coverage: 100% (required: 100%)"
done
