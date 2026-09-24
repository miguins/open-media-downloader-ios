#!/bin/sh
set -eu

if [ -z "${OMDI_REAL_SMOKE_URL:-}" ]; then
	echo "real smoke URL is required" >&2
	exit 2
fi

app_binary=${APP_BINARY:-/home/omdi/.cache/go-build/omdi-dev}
api_key=
owner_id=

cleanup() {
	if [ -n "$owner_id" ]; then
		docker compose exec -T app "$app_binary" jobs purge --yes --owner "$owner_id" >/dev/null 2>&1 || true
		docker compose exec -T app "$app_binary" keys revoke "$owner_id" >/dev/null 2>&1 || true
	fi
	docker compose down --remove-orphans >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM

docker compose up --build --detach --wait app >/dev/null
api_key=$(docker compose exec -T app "$app_binary" keys create --name "real-smoke-$(date +%s)" 2>/dev/null)
owner_id=$(printf '%s' "$api_key" | awk -F_ '{print $2}')
if [ -z "$api_key" ] || [ -z "$owner_id" ]; then
	echo "real smoke setup failed" >&2
	exit 1
fi
export OMDI_API_KEY="$api_key" OMDI_REAL_SMOKE_URL

docker compose exec -T -e OMDI_API_KEY -e OMDI_REAL_SMOKE_URL app python3 - <<'PY'
import json
import os
import time
import urllib.error
import urllib.parse
import urllib.request

base = "http://127.0.0.1:8080"
headers = {"Authorization": "Bearer " + os.environ["OMDI_API_KEY"], "Content-Type": "application/json"}
payload = json.dumps({"url": os.environ["OMDI_REAL_SMOKE_URL"]}).encode()
deadline = time.monotonic() + 630
downloads = []
try:
    request = urllib.request.Request(base + "/v1/jobs", data=payload, headers=headers, method="POST")
    with urllib.request.urlopen(request, timeout=15) as response:
        location = response.headers["Location"]
    while time.monotonic() < deadline:
        request = urllib.request.Request(base + location, headers=headers)
        with urllib.request.urlopen(request, timeout=15) as response:
            job = json.load(response)
        if job["status"] == "succeeded":
            break
        if job["status"] in ("failed", "canceled"):
            raise RuntimeError("real smoke job failed")
        time.sleep(1)
    else:
        raise RuntimeError("real smoke job timed out")
    for index, item in enumerate(job["items"]):
        path = "/tmp/omdi-real-smoke-%d" % index
        downloads.append(path)
        size = 0
        # download_url is absolute under OMDI_PUBLIC_URL; fetch its path from the local listener.
        download = base + urllib.parse.urlsplit(item["download_url"]).path
        with urllib.request.urlopen(download, timeout=30) as response, open(path, "wb") as output:
            while True:
                chunk = response.read(64 * 1024)
                if not chunk:
                    break
                output.write(chunk)
                size += len(chunk)
        if size == 0:
            raise RuntimeError("real smoke download was empty")
except (KeyError, OSError, ValueError, urllib.error.HTTPError, urllib.error.URLError) as error:
    raise RuntimeError("real smoke request failed") from None
finally:
    for path in downloads:
        try:
            os.unlink(path)
        except FileNotFoundError:
            pass
print("real smoke: ok")
PY
