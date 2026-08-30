FROM golang:1.24.1-alpine3.21@sha256:43c094ad24b6ac0546c62193baeb3e6e49ce14d3250845d166c77c25f64b0386 AS build
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
