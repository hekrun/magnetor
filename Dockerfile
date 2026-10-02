FROM golang:1.24-bookworm AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -o /out/magnetor .

FROM debian:bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates util-linux \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --create-home --uid 10001 app

WORKDIR /app
COPY --from=build /out/magnetor /app/magnetor
COPY web /app/web
COPY README.md /app/README.md
COPY docker-entrypoint.sh /app/docker-entrypoint.sh
COPY docker-ctd.sh /usr/local/bin/ctd
RUN chmod 755 /app/docker-entrypoint.sh /usr/local/bin/ctd \
    && mkdir -p /data/downloads /data/state \
    && chown -R app:app /app /data

ENV DOWNLOAD_DIR=/data/downloads
ENV STATE_DIR=/data/state
EXPOSE 8080
VOLUME ["/data"]

ENTRYPOINT ["/app/docker-entrypoint.sh"]
