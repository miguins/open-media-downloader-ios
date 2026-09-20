COMPOSE := docker compose
TOOLS_RUN := $(COMPOSE) run --rm --no-deps tools
TRIVY_IMAGE := aquasec/trivy:0.74.0@sha256:62b1e65e8869bc4b4c6aa4fa2b21595256c7c2f6018a9d9ad61caf87187c1969

.PHONY: bootstrap fmt fmt-check test coverage lint vuln secret-scan build docker-build image-scan smoke compose-up compose-down collection-test ci

bootstrap:
	command -v docker >/dev/null
	$(COMPOSE) version
	$(COMPOSE) build tools
	$(TOOLS_RUN) go mod download
	$(TOOLS_RUN) sh -ec 'go version; python --version; go tool golangci-lint version; go tool govulncheck -version; yt-dlp --version; gallery-dl --version; ffmpeg -version | head -n 1; ffprobe -version | head -n 1'

fmt:
	$(TOOLS_RUN) sh -ec 'gofmt -w $$(find cmd internal -type f -name "*.go")'

fmt-check:
	$(TOOLS_RUN) sh -ec 'files=$$(gofmt -l cmd internal); test -z "$$files" || { printf "%s\n" "$$files"; exit 1; }'

test:
	$(TOOLS_RUN) go test -race -count=1 ./...

coverage:
	$(TOOLS_RUN) sh -ec 'go test -covermode=atomic -coverpkg=./internal/... -coverprofile=/tmp/coverage.out ./internal/... && ./scripts/check-coverage.sh /tmp/coverage.out'

lint:
	$(TOOLS_RUN) sh -ec 'go vet ./... && go tool golangci-lint run'

vuln:
	$(TOOLS_RUN) go tool govulncheck ./...

secret-scan:
	docker run --rm --volume "$(CURDIR):/workspace:ro" $(TRIVY_IMAGE) fs --scanners secret --exit-code 1 --skip-dirs /workspace/.git /workspace

build:
	$(TOOLS_RUN) go build -trimpath -o /tmp/omdi ./cmd/omdi

docker-build:
	docker build --target runtime --tag omdi:local .

image-scan: docker-build
	docker run --rm --volume /var/run/docker.sock:/var/run/docker.sock:ro $(TRIVY_IMAGE) image --scanners vuln --exit-code 1 --ignore-unfixed --severity HIGH,CRITICAL omdi:local

smoke: docker-build
	@set -eu; container_id=$$(docker run --rm --detach omdi:local); trap 'docker stop "$$container_id" >/dev/null 2>&1 || true' EXIT INT TERM; attempts=0; until docker exec "$$container_id" python3 -c 'import json, urllib.request; response = urllib.request.urlopen("http://127.0.0.1:8080/healthz", timeout=2); assert response.status == 200; assert json.load(response) == {"status": "ok"}' >/dev/null 2>&1; do attempts=$$((attempts + 1)); test "$$attempts" -lt 30; sleep 1; done

compose-up:
	$(COMPOSE) up --build

compose-down:
	$(COMPOSE) down --remove-orphans

collection-test:
	@set -eu; trap '$(COMPOSE) down --remove-orphans' EXIT INT TERM; $(COMPOSE) up --build --detach --wait app; $(COMPOSE) run --rm bruno

ci:
	$(MAKE) fmt-check
	$(MAKE) test
	$(MAKE) coverage
	$(MAKE) lint
	$(MAKE) vuln
	$(MAKE) secret-scan
	$(MAKE) build
	$(MAKE) docker-build smoke image-scan
	$(MAKE) collection-test
