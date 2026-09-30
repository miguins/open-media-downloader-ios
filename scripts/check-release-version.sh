#!/bin/sh
# Validate a release tag and require it to match info.version in the OpenAPI
# document. Prints version=<version> for $GITHUB_OUTPUT.
set -eu

tag=${1-}
openapi=${OMDI_OPENAPI_PATH:-docs/openapi.yaml}
number='(0|[1-9][0-9]*)'
newline='
'
case $tag in *"$newline"*) tag= ;; esac
if ! printf '%s\n' "$tag" | grep -Eqx "v$number\.$number\.$number"; then
	echo "release tag must have the form vMAJOR.MINOR.PATCH" >&2
	exit 1
fi
version=${tag#v}
documented=$(awk '
	/^info:/ { in_info = 1; next }
	in_info && /^[^ ]/ { exit }
	in_info && /^  version:/ { value = $2; gsub(/["'\'']/, "", value); print value; exit }
' "$openapi")
if [ "$documented" != "$version" ]; then
	echo "release tag does not match info.version in the OpenAPI document" >&2
	exit 1
fi
echo "version=$version"
