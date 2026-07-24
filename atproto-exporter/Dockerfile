# Build a static binary, then ship it on distroless (with CA certs for TLS).
FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/atproto-exporter ./cmd/atproto-exporter

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/atproto-exporter /atproto-exporter
ENV ATPROTO_CURSOR_DIR=/data
EXPOSE 9200
VOLUME ["/data"]
ENTRYPOINT ["/atproto-exporter"]
