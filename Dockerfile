FROM golang:1.26.3-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /zwoice ./cmd/zwoice

FROM alpine:3.23
RUN apk add --no-cache ca-certificates su-exec && adduser -D -u 10001 zwoice
COPY --from=build /zwoice /usr/local/bin/zwoice
COPY scripts/server-entrypoint.sh /usr/local/bin/server-entrypoint
RUN chmod 755 /usr/local/bin/server-entrypoint
ENV PORT=8080 ZWOICE_DATA_DIR=/data
EXPOSE 8080
ENTRYPOINT ["server-entrypoint"]
