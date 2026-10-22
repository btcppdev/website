# syntax=docker/dockerfile:1

FROM golang:1.25.10-alpine

WORKDIR /app

# This copied guard also invalidates the package layer when trust policy changes.
COPY cmd/container-trust/main.go /tmp/container-trust.go
RUN apk add --no-cache --upgrade make 'ca-certificates>=20261020-r0' 'ca-certificates-bundle>=20261020-r0' chromium ffmpeg \
    && GO111MODULE=off GOTOOLCHAIN=local go run /tmp/container-trust.go prune /usr/share/ca-certificates /usr/local/share/ca-certificates \
    && update-ca-certificates --fresh \
    && GO111MODULE=off GOTOOLCHAIN=local go run /tmp/container-trust.go verify /etc/ssl/certs

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
	--mount=type=cache,target=/root/.cache/go-build \
	make build \
    && mkdir -p static/security \
    && printf 'System CA trust policy: 2026-10-22\n' > static/security/container-trust.txt \
    && GO111MODULE=off GOTOOLCHAIN=local go run /tmp/container-trust.go verify /etc/ssl/certs >> static/security/container-trust.txt \
    && apk info -v ca-certificates ca-certificates-bundle >> static/security/container-trust.txt

CMD [ "./target/btcpp-web" ]
