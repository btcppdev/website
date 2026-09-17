# syntax=docker/dockerfile:1

FROM golang:1.27.3-alpine3.24

WORKDIR /app

# This copied guard also invalidates the package layer when trust policy changes.
COPY cmd/container-trust/main.go /tmp/container-trust.go
RUN apk upgrade --no-cache \
    && apk add --no-cache --upgrade make 'ca-certificates>=20261020-r0' 'ca-certificates-bundle>=20261020-r0' chromium ffmpeg \
    && GO111MODULE=off GOTOOLCHAIN=local go run /tmp/container-trust.go prune /usr/share/ca-certificates /usr/local/share/ca-certificates \
    && update-ca-certificates --fresh \
    && GO111MODULE=off GOTOOLCHAIN=local go run /tmp/container-trust.go verify /etc/ssl/certs

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
	--mount=type=cache,target=/root/.cache/go-build \
	make build build-ops \
    && mkdir -p static/security \
    && printf 'System CA trust policy: 2026-10-22\n' > static/security/container-trust.txt \
    && GO111MODULE=off GOTOOLCHAIN=local go run /tmp/container-trust.go verify /etc/ssl/certs >> static/security/container-trust.txt \
    && apk info -v | grep -E '^ca-certificates(-bundle)?-[0-9]' >> static/security/container-trust.txt \
    && { go version; cat /etc/alpine-release; apk info -v; } > static/security/runtime-versions.txt \
    && awk '/^[PVAoLcpD]:/ || /^$/' /lib/apk/db/installed > static/security/apk-installed.txt

CMD [ "./target/btcpp-web" ]
