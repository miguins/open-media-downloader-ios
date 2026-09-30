#!/bin/sh
# Test scripts/check-release-version.sh against a synthetic OpenAPI document.
set -eu

test_dir=$(mktemp -d)
trap 'rm -rf "$test_dir"' EXIT INT TERM
printf 'openapi: 3.2.1\ninfo:\n  title: Test\n  version: 1.20.3\n' >"$test_dir/openapi.yaml"
check() {
	OMDI_OPENAPI_PATH="$test_dir/openapi.yaml" sh scripts/check-release-version.sh "$1"
}

output=$(check v1.20.3)
test "$output" = "version=1.20.3" || { echo "matching tag printed: $output" >&2; exit 1; }

for tag in v1.20.4 1.20.3 v01.20.3 v1.020.3 v1.20.03 v1.20.3-rc.1 v1.20.3+build v1.20 v1.20.3.4 V1.20.3 '' 'v1.20.3 ' 'v1.20.3
x'; do
	if check "$tag" >"$test_dir/out" 2>"$test_dir/err"; then
		echo "accepted invalid tag: '$tag'" >&2
		exit 1
	fi
	test ! -s "$test_dir/out" || { echo "rejected tag '$tag' printed output" >&2; exit 1; }
done

printf 'openapi: 3.2.1\ninfo:\n  title: Test\n' >"$test_dir/openapi.yaml"
if check v1.20.3 >/dev/null 2>&1; then
	echo "accepted a document without a version" >&2
	exit 1
fi
echo "release version check: ok"
