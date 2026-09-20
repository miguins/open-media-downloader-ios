#!/bin/sh
set -eu

profile=${1:?usage: check-coverage.sh COVERAGE_PROFILE}
minimum=${COVERAGE_MIN:-90}
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
