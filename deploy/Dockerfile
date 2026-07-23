# syntax=docker/dockerfile:1.7
FROM golang:1.26-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . ./
RUN CGO_ENABLED=1 go build -trimpath -buildvcs=false -ldflags='-s -w' -o /out/llm-proxy-server ./cmd/llm-proxy-server

FROM debian:bookworm-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl openssl \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --gid 65532 llmproxy \
    && useradd --uid 65532 --gid 65532 --no-create-home --shell /usr/sbin/nologin llmproxy \
    && install -d --owner=65532 --group=65532 --mode=0750 /var/lib/llm-proxy
COPY --from=build --chown=65532:65532 /out/llm-proxy-server /usr/local/bin/llm-proxy-server
USER 65532:65532
WORKDIR /var/lib/llm-proxy
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/llm-proxy-server"]
CMD ["serve"]
