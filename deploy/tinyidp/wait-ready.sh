#!/bin/sh
set -eu

# Caddy is started concurrently with this one-shot gate. Give its local CA and
# TLS listener a bounded settle period before the first verified probe.
sleep 3

for _ in $(seq 1 120); do
  if curl --fail --silent --show-error \
    --cacert /trust/caddy-local-root.crt \
    --resolve idp.localhost:18443:172.30.0.2 \
    https://idp.localhost:18443/readyz >/dev/null; then
    exit 0
  fi
  sleep 1
done

echo "tiny-idp did not become ready through verified Caddy TLS" >&2
exit 1
