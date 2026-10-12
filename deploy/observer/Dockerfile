# Private operator image. Do not publish the image or bundle it into clients.
# Licensing and real-data qualification gates: docs/OBSERVERS.md.
FROM golang:1.26.3-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /zwoice ./cmd/zwoice

FROM node:24-bookworm-slim
ARG TARGETARCH
RUN test "$TARGETARCH" = amd64
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates curl libasound2 libatk-bridge2.0-0 libatk1.0-0 libcups2 \
    libdbus-1-3 libdrm2 libgbm1 libgtk-3-0 libnss3 libxcomposite1 \
    libxdamage1 libxfixes3 libxkbcommon0 libxrandr2 \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /opt
RUN curl -fL --retry 3 https://github.com/SauceLLC/sauce4zwift-releases/releases/download/v2.3.4/sauce4zwift-2.3.4.AppImage -o sauce.AppImage \
    && echo 'f811e7418268247fd44bc7f34acb9d9b5e40765043d70315d74e6edc2d64021b  sauce.AppImage' | sha256sum -c - \
    && chmod 755 sauce.AppImage && ./sauce.AppImage --appimage-extract >/dev/null \
    && mv squashfs-root sauce && rm sauce.AppImage \
    && chmod -R a+rX /opt/sauce
COPY --from=build /zwoice /usr/local/bin/zwoice
COPY deploy/observer/init.mjs /opt/zwoice/init.mjs
RUN mkdir -p /data && chown 1000:1000 /data
USER 1000:1000
ENV ZWOICE_OBSERVER_CONFIG=/run/secrets/observer.json
# No EXPOSE. Sauce's internal APIs must never be publicly reachable.
ENTRYPOINT ["zwoice", "observer"]
