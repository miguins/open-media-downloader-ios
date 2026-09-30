#!/bin/sh
# Start compose.yaml in an isolated project, check its hardening, health, readiness,
# and key management, and remove the project with its volumes on exit.
set -eu

work=$(mktemp -d)
: >"$work/env"
compose() {
	docker compose --project-name omdi-production-smoke --env-file "$work/env" \
		-f compose.yaml -f docker/compose.production-smoke.yaml "$@"
}
cleanup() {
	compose down --volumes --remove-orphans >/dev/null 2>&1 || true
	rm -rf "$work"
}
trap cleanup EXIT INT TERM

sh scripts/check-compose.sh --project-name omdi-production-smoke --env-file "$work/env" \
	-f compose.yaml -f docker/compose.production-smoke.yaml
compose up --build --detach --wait --wait-timeout 180 >/dev/null 2>&1 || {
	echo "production service did not become healthy" >&2
	compose logs app >&2 || true
	exit 1
}
compose exec -T app python3 -c '
import json, urllib.request
for path, body in (("/healthz", {"status": "ok"}), ("/readyz", {"status": "ready"})):
    response = urllib.request.urlopen("http://127.0.0.1:8080" + path, timeout=5)
    assert response.status == 200 and json.load(response) == body, path
' || {
	echo "production health or readiness check failed" >&2
	exit 1
}
compose exec -T app omdi keys create --name production-smoke >"$work/key" 2>/dev/null
grep -Eq '^omdi_[a-z2-7]{26}_[A-Za-z0-9_-]+$' "$work/key" || {
	echo "production key creation failed" >&2
	exit 1
}
compose exec -T app omdi keys list 2>/dev/null | grep -q 'production-smoke' || {
	echo "production key listing failed" >&2
	exit 1
}
echo "production smoke: ok"
