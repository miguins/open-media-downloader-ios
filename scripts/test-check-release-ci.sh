#!/bin/sh
# Test scripts/check-release-ci.sh with a fake gh that replays scripted responses.
set -eu

test_dir=$(mktemp -d)
trap 'rm -rf "$test_dir"' EXIT INT TERM
cat >"$test_dir/gh" <<'SH'
#!/bin/sh
printf '%s\n' "$*" >>"$FAKE_GH_LOG"
case "$*" in
  *compare*) printf '%s\n' "$FAKE_COMPARE" ;;
  *actions/workflows/ci.yml/runs*)
    count=$(cat "$FAKE_GH_COUNT" 2>/dev/null || echo 0)
    count=$((count + 1))
    echo "$count" >"$FAKE_GH_COUNT"
    line=$(sed -n "${count}p" "$FAKE_RUNS")
    [ -n "$line" ] || line=$(tail -n 1 "$FAKE_RUNS")
    printf '%s\n' "$line" ;;
  *) exit 1 ;;
esac
SH
chmod +x "$test_dir/gh"
sha=0123456789abcdef0123456789abcdef01234567
export PATH="$test_dir:$PATH" GITHUB_REPOSITORY=owner/repo OMDI_CI_POLL_SECONDS=0 OMDI_CI_WAIT_SECONDS=3

# run <compare status> <runs...>: replay CI run states in order, repeating the last one.
run() {
	export FAKE_COMPARE=$1 FAKE_GH_LOG="$test_dir/log" FAKE_GH_COUNT="$test_dir/count" FAKE_RUNS="$test_dir/runs"
	shift
	: >"$FAKE_GH_LOG"
	rm -f "$FAKE_GH_COUNT"
	printf '%s\n' "$@" >"$FAKE_RUNS"
	sh scripts/check-release-ci.sh "$sha" >"$test_dir/out" 2>"$test_dir/err"
}
expect_pass() {
	run "$@" || { echo "expected pass: $*" >&2; cat "$test_dir/err" >&2; exit 1; }
}
expect_fail() {
	if run "$@"; then echo "expected failure: $*" >&2; exit 1; fi
}

expect_pass identical "completed success"
expect_pass ahead "null null" "queued null" "in_progress null" "completed success"
grep -q "head_sha=$sha" "$test_dir/log" || { echo "runs were not filtered by commit" >&2; exit 1; }
grep -q 'branch=main' "$test_dir/log" || { echo "runs were not filtered by branch" >&2; exit 1; }
grep -q 'event=push' "$test_dir/log" || { echo "runs were not filtered by event" >&2; exit 1; }

expect_fail identical "completed failure"
expect_fail identical "in_progress null" "completed cancelled"
expect_fail behind "completed success"
expect_fail diverged "completed success"
test ! -s "$test_dir/count" || { echo "checked CI for a commit that is not on main" >&2; exit 1; }
expect_fail identical "in_progress null"
grep -q 'timed out' "$test_dir/err" || { echo "timeout was not reported" >&2; exit 1; }

for bad in 0123 "$sha;rm" "$(printf '%s' "$sha" | tr a-f A-F)" ""; do
	if sh scripts/check-release-ci.sh "$bad" >/dev/null 2>&1; then
		echo "accepted invalid commit: '$bad'" >&2
		exit 1
	fi
done
echo "release CI check: ok"
