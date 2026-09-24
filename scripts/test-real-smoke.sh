#!/bin/sh
set -eu

test_dir=$(mktemp -d)
trap 'rm -rf "$test_dir"' EXIT INT TERM
log=$test_dir/docker.log
cat >"$test_dir/docker" <<'SH'
#!/bin/sh
printf '%s\n' "$*" >>"$FAKE_DOCKER_LOG"
case "$*" in
  *"keys create"*) printf '%s\n' 'omdi_syntheticowner_syntheticsecret' ;;
  *"python3 -"*) [ "${FAKE_FAIL_PYTHON:-0}" = 0 ] || exit 1; printf '%s\n' 'real smoke: ok' ;;
esac
SH
chmod +x "$test_dir/docker"
export PATH="$test_dir:$PATH" FAKE_DOCKER_LOG=$log

if env -u OMDI_REAL_SMOKE_URL sh scripts/real-smoke.sh >"$test_dir/out" 2>"$test_dir/err"; then
	echo "missing URL was accepted" >&2; exit 1
fi
test ! -e "$log"

OMDI_REAL_SMOKE_URL='https://example.invalid/post?secret=synthetic' sh scripts/real-smoke.sh >"$test_dir/out" 2>"$test_dir/err"
grep -q 'jobs purge --yes --owner' "$log"
grep -q 'keys revoke' "$log"
grep -q 'down --remove-orphans' "$log"
! grep -q 'example.invalid\|syntheticsecret' "$test_dir/out" "$test_dir/err"

: >"$log"
if OMDI_REAL_SMOKE_URL='https://example.invalid/post?secret=synthetic' FAKE_FAIL_PYTHON=1 sh scripts/real-smoke.sh >"$test_dir/out" 2>"$test_dir/err"; then
	echo "submission failure was accepted" >&2; exit 1
fi
grep -q 'jobs purge --yes --owner' "$log"
grep -q 'keys revoke' "$log"
grep -q 'down --remove-orphans' "$log"
! grep -q 'example.invalid\|syntheticsecret' "$test_dir/out" "$test_dir/err"
