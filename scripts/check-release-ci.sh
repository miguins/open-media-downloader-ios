#!/bin/sh
# Require a release commit to be on main and the CI workflow to have succeeded for
# it on a push to main. Waits while CI is queued or running, for at most
# OMDI_CI_WAIT_SECONDS.
set -eu

sha=${1-}
repo=${GITHUB_REPOSITORY:?GITHUB_REPOSITORY is required}
wait_seconds=${OMDI_CI_WAIT_SECONDS:-3600}
poll_seconds=${OMDI_CI_POLL_SECONDS:-30}
if ! printf '%s\n' "$sha" | grep -Eqx '[0-9a-f]{40}'; then
	echo "release commit must be a full lowercase SHA" >&2
	exit 1
fi

# Comparing the commit with main reports "ahead" or "identical" only when main contains it.
if ! comparison=$(gh api "repos/$repo/compare/$sha...main" --jq .status); then
	echo "unable to compare the release commit with main" >&2
	exit 1
fi
case $comparison in
ahead | identical) ;;
*)
	echo "release commit is not on main" >&2
	exit 1
	;;
esac

elapsed=0
while :; do
	if ! state=$(gh api "repos/$repo/actions/workflows/ci.yml/runs?head_sha=$sha&branch=main&event=push&per_page=1" \
		--jq '.workflow_runs[0] | "\(.status) \(.conclusion)"'); then
		echo "unable to read CI runs for the release commit" >&2
		exit 1
	fi
	case $state in
	"completed success")
		echo "CI passed for the release commit"
		exit 0
		;;
	completed\ *)
		echo "CI did not succeed for the release commit" >&2
		exit 1
		;;
	esac
	if [ "$elapsed" -ge "$wait_seconds" ]; then
		echo "timed out waiting for CI on the release commit" >&2
		exit 1
	fi
	sleep "$poll_seconds"
	# Count at least one second per attempt, so a zero interval still reaches the limit.
	step=$poll_seconds
	[ "$step" -gt 0 ] || step=1
	elapsed=$((elapsed + step))
done
