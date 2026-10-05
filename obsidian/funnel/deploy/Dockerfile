FROM golang:1.27.1-alpine@sha256:8a5910f31396cd4d89662f56c68b3ae31d374308270a1c3bd96672ee5ed43414 AS build
WORKDIR /src
ENV GOWORK=off GOTOOLCHAIN=local CGO_ENABLED=0
COPY go.mod ./
COPY cmd/ ./cmd/
COPY internal/ ./internal/
RUN go test ./... && go build -trimpath -o /out/funnel ./cmd/funnel

FROM alpine:3.23.3@sha256:25109184c71bdad752c8312a8623239686a9a2071e8825f20acb8f2198c3f659
RUN apk add --no-cache ca-certificates tzdata git openssh-client \
    && addgroup -g 10001 funnel && adduser -D -H -u 10001 -G funnel funnel
COPY --from=build /out/funnel /usr/local/bin/funnel
COPY deploy/publish.sh /usr/local/bin/funnel-publish
USER 10001:10001
ENTRYPOINT ["/usr/local/bin/funnel"]
