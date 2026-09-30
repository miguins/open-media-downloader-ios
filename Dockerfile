ARG GO_IMAGE=golang:1.27.1-trixie@sha256:433790e515d27dc6003e847e644cc0af956985cf315c1c58a3b73ee2dd305183
ARG DEBIAN_IMAGE=debian:trixie-slim@sha256:a99cfc517144bc59b1978475ec53b46ecabec7e43635402ee5b77cc54cd1b20a
ARG PYTHON_IMAGE=python:3.14.7-slim-trixie@sha256:caaf356f40667c496d405780745b9ac25771c189a51dfcc42430d531ea09f8a2
ARG FFMPEG_VERSION=9.0.2
ARG FFMPEG_SHA256=8c3850283eb25fa026482078a04051e0be17347b09ef81a0849bec15a96e002e
ARG FFMPEG_SIGNING_FINGERPRINT=FCF986EA15E6E293A5644F10B4322F04D67658D8

FROM ${DEBIAN_IMAGE} AS ffmpeg-builder
ARG FFMPEG_VERSION
ARG FFMPEG_SHA256
ARG FFMPEG_SIGNING_FINGERPRINT
RUN apt-get update \
    && apt-get install --yes --no-install-recommends \
        build-essential ca-certificates curl gnupg nasm pkg-config xz-utils yasm \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /tmp/ffmpeg
RUN set -eu; \
    curl --fail --show-error --silent --location --output ffmpeg.tar.xz \
        "https://ffmpeg.org/releases/ffmpeg-${FFMPEG_VERSION}.tar.xz"; \
    curl --fail --show-error --silent --location --output ffmpeg.tar.xz.asc \
        "https://ffmpeg.org/releases/ffmpeg-${FFMPEG_VERSION}.tar.xz.asc"; \
    curl --fail --show-error --silent --location --output ffmpeg-devel.asc \
        "https://ffmpeg.org/ffmpeg-devel.asc"; \
    echo "${FFMPEG_SHA256}  ffmpeg.tar.xz" | sha256sum --check --strict; \
    fingerprint="$(gpg --batch --show-keys --with-colons ffmpeg-devel.asc | awk -F: '$1 == "fpr" { print $10; exit }')"; \
    test "$fingerprint" = "$FFMPEG_SIGNING_FINGERPRINT"; \
    gpg --batch --import ffmpeg-devel.asc; \
    gpg --batch --verify ffmpeg.tar.xz.asc ffmpeg.tar.xz; \
    tar --extract --xz --file ffmpeg.tar.xz --strip-components=1
RUN ./configure \
        --prefix=/opt/ffmpeg \
        --disable-autodetect \
        --disable-debug \
        --disable-doc \
        --disable-ffplay \
        --disable-shared \
        --enable-static \
        --enable-small \
    && make -j"$(nproc)" \
    && make install \
    && /opt/ffmpeg/bin/ffmpeg -version \
    && /opt/ffmpeg/bin/ffprobe -version \
    && (ldd /opt/ffmpeg/bin/ffmpeg > /tmp/ffmpeg-ldd.txt 2>&1 || true) \
    && (ldd /opt/ffmpeg/bin/ffprobe > /tmp/ffprobe-ldd.txt 2>&1 || true) \
    && ! grep -q "not found" /tmp/ffmpeg-ldd.txt \
    && ! grep -q "not found" /tmp/ffprobe-ldd.txt

FROM ${PYTHON_IMAGE} AS python-runtime

FROM ${GO_IMAGE} AS go-toolchain

FROM python-runtime AS media-tools-builder
COPY docker/media-tools.lock /tmp/media-tools.lock
RUN python -m venv /opt/media-tools \
    && /opt/media-tools/bin/pip install \
        --no-cache-dir \
        --require-hashes \
        --requirement /tmp/media-tools.lock \
    && /opt/media-tools/bin/yt-dlp --version \
    && /opt/media-tools/bin/gallery-dl --version

FROM python-runtime AS development
ARG DEV_UID=1000
ARG DEV_GID=1000
COPY --from=go-toolchain /usr/local/go/ /usr/local/go/
COPY --from=ffmpeg-builder /opt/ffmpeg/ /opt/ffmpeg/
COPY --from=media-tools-builder /opt/media-tools/ /opt/media-tools/
ENV PATH="/usr/local/go/bin:/opt/ffmpeg/bin:/opt/media-tools/bin:${PATH}" \
    GOMODCACHE=/go/pkg/mod \
    GOCACHE=/home/omdi/.cache/go-build \
    GOFLAGS=-buildvcs=false
RUN apt-get update \
    && apt-get install --yes --no-install-recommends build-essential ca-certificates git \
    && rm -rf /var/lib/apt/lists/* \
    && test "${DEV_UID}" -gt 0 \
    && test "${DEV_GID}" -gt 0 \
    && groupadd --gid "${DEV_GID}" omdi \
    && useradd --uid "${DEV_UID}" --gid "${DEV_GID}" --create-home --shell /bin/sh omdi \
    && mkdir -p /workspace /data /go/pkg/mod /home/omdi/.cache/go-build \
    && chown -R omdi:omdi /workspace /data /go /home/omdi
WORKDIR /workspace
USER omdi

FROM development AS builder
COPY --chown=omdi:omdi go.mod go.sum ./
RUN go mod download
COPY --chown=omdi:omdi . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /tmp/omdi ./cmd/omdi

FROM ${PYTHON_IMAGE} AS runtime
LABEL org.opencontainers.image.title="OpenMediaDownloaderIOS" \
      org.opencontainers.image.description="Self-hosted media download API for iOS Shortcuts" \
      org.opencontainers.image.source="https://github.com/miguins/open-media-downloader-ios" \
      org.opencontainers.image.licenses="MIT"
COPY --from=ffmpeg-builder /opt/ffmpeg/ /opt/ffmpeg/
COPY --from=media-tools-builder /opt/media-tools/ /opt/media-tools/
COPY --from=builder /tmp/omdi /usr/local/bin/omdi
ENV PATH="/opt/ffmpeg/bin:/opt/media-tools/bin:${PATH}"
RUN rm -rf \
        /usr/local/lib/python3.14/ensurepip \
        /usr/local/lib/python3.14/site-packages/pip \
        /usr/local/lib/python3.14/site-packages/pip-*.dist-info \
        /opt/media-tools/lib/python3.14/site-packages/pip \
        /opt/media-tools/lib/python3.14/site-packages/pip-*.dist-info \
    && rm -f \
        /usr/local/bin/pip \
        /usr/local/bin/pip3 \
        /usr/local/bin/pip3.14 \
        /opt/media-tools/bin/pip \
        /opt/media-tools/bin/pip3 \
        /opt/media-tools/bin/pip3.14 \
    && groupadd --gid 10001 omdi \
    && useradd --uid 10001 --gid 10001 --create-home --shell /bin/sh omdi \
    && mkdir -p /data \
    && chown omdi:omdi /data
USER omdi
WORKDIR /data
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
    CMD ["python3", "-c", "import json, urllib.request; response = urllib.request.urlopen('http://127.0.0.1:8080/healthz', timeout=2); assert response.status == 200; assert json.load(response) == {'status': 'ok'}"]
ENTRYPOINT ["/usr/local/bin/omdi"]
CMD ["serve"]
