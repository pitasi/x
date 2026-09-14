# syntax=docker/dockerfile:1

FROM golang:1.25.5-alpine3.21 AS build-env
WORKDIR /build
ENV CGO_ENABLED=0
RUN --mount=type=bind,source=.,target=.,readonly \
    --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    go build -o /app .

FROM alpine:3.24
RUN apk add --no-cache --virtual mailcap
COPY --from=build-env /app /usr/bin/app
CMD ["app"]

