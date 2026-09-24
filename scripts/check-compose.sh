#!/bin/sh
set -eu

app=$(docker compose config | awk '/^  app:$/ { found=1; next } found && /^  [A-Za-z0-9_-]+:$/ { exit } found { print }')
printf '%s\n' "$app" | grep -Eq '^    mem_limit: (1073741824|"1073741824")$' || {
	echo "app memory limit must be 1073741824 bytes" >&2
	exit 1
}
printf '%s\n' "$app" | grep -Eq '^    pids_limit: (64|"64")$' || {
	echo "app PID limit must be 64" >&2
	exit 1
}
