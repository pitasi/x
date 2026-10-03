FROM golang:1.25.5-alpine3.21@sha256:b4dbd292a0852331c89dfd64e84d16811f3e3aae4c73c13d026c4d200715aff6 AS build
WORKDIR /src/togo
COPY togo/go.mod togo/go.sum ./
RUN go mod download
COPY togo/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /togo ./cmd/togo

FROM scratch
COPY --from=build /togo /togo
COPY togo-example/content /content
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/togo", "/content", "-laddr", "8080"]
