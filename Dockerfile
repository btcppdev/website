# syntax=docker/dockerfile:1

FROM golang:1.25.10-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
	--mount=type=cache,target=/root/.cache/go-build \
	CGO_ENABLED=0 GOSUMDB=sum.golang.org go build \
		-trimpath \
		-ldflags="-s -w" \
		-o /out/btcpp-web \
		./cmd/web/main.go

FROM alpine:3.22

WORKDIR /app

RUN apk add --no-cache ca-certificates chromium ffmpeg tzdata

# Keep the large, infrequently changed static tree below application code in
# the layer stack. Most deploys can then reuse its layer and upload only the
# changed templates, migrations, or binary.
COPY static ./static
COPY templates ./templates
COPY db/migrations ./db/migrations
COPY --from=build /out/btcpp-web ./btcpp-web

CMD [ "./btcpp-web" ]
