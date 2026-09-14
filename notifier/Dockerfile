# syntax=docker/dockerfile:1

FROM golang:1.23-alpine3.20 AS build-env
WORKDIR /build
ENV CGO_ENABLED=0
RUN --mount=type=bind,source=.,target=.,readonly \
    --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    go build -o /app .

FROM alpine:3.24
COPY --from=build-env /app /usr/bin/app
CMD ["app"]

