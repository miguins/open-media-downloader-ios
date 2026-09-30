#!/bin/sh
# Verify the hardening and limits of the rendered app service. Arguments select the
# Compose files and project, for example: check-compose.sh -f compose.yaml
set -eu

if [ "$#" -eq 0 ]; then
	set -- -f compose.development.yaml
fi
app=$(docker compose "$@" config | awk '/^  app:$/ { found=1; next } found && /^  [A-Za-z0-9_-]+:$/ { exit } found { print }')
require() {
	printf '%s\n' "$app" | grep -Eq "$1" || {
		echo "$2" >&2
		exit 1
	}
}
require '^    mem_limit: (1073741824|"1073741824")$' "app memory limit must be 1073741824 bytes"
require '^    pids_limit: (64|"64")$' "app PID limit must be 64"
require '^    read_only: true$' "app root filesystem must be read-only"
require '^      - ALL$' "app must drop all capabilities"
require '^      - no-new-privileges:true$' "app must set no-new-privileges"
