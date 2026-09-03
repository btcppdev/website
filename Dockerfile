# syntax=docker/dockerfile:1

FROM golang:1.25.10-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

# The web binary has no build-time dependency on templates or the 283 MB
# static tree. Keep those out of the compiler layer so content-only changes do
# not invalidate the Go build cache or copy all media through the build stage.
COPY cmd ./cmd
COPY external ./external
COPY internal ./internal
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

# Keep large archival media in independent, early layers. A CSS, JavaScript,
# template, or binary edit can then reuse both media layers rather than
# rebuilding and uploading the entire static tree.
COPY static/atx23 ./static/atx23
COPY static/img ./static/img
COPY static/fonts ./static/fonts
COPY static/favicon ./static/favicon
COPY static/css ./static/css
COPY static/js ./static/js
COPY static/llms.txt static/robots.txt ./static/
COPY templates ./templates
COPY db/migrations ./db/migrations
COPY --from=build /out/btcpp-web ./btcpp-web

CMD [ "./btcpp-web" ]
