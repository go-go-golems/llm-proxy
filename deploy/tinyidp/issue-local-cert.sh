#!/bin/sh
set -eu

authority=/authority/caddy/pki/authorities/local
root=$authority/root.crt
intermediate=$authority/intermediate.crt
intermediate_key=$authority/intermediate.key
certificate=/certs/server.crt
private_key=/certs/server.key

if ! command -v openssl >/dev/null 2>&1; then
  echo "certificate issuer image does not contain openssl" >&2
  exit 1
fi

for path in "$root" "$intermediate" "$intermediate_key"; do
  if [ ! -s "$path" ]; then
    echo "persistent local Caddy authority is incomplete" >&2
    exit 1
  fi
done

# Reuse a still-valid certificate with the exact required hostnames. Keeping
# issuance idempotent avoids needless private-key and serial churn on restart.
if [ -s "$certificate" ] && [ -s "$private_key" ] \
  && openssl x509 -in "$certificate" -noout -checkend 604800 >/dev/null 2>&1 \
  && openssl x509 -in "$certificate" -noout -checkhost idp.localhost >/dev/null 2>&1 \
  && openssl x509 -in "$certificate" -noout -checkhost proxy.localhost >/dev/null 2>&1 \
  && openssl verify -CAfile "$root" -untrusted "$intermediate" "$certificate" >/dev/null 2>&1; then
  exit 0
fi

work=$(mktemp -d /certs/.issue.XXXXXX)
trap 'rm -rf "$work"' EXIT HUP INT TERM

cat >"$work/extensions.cnf" <<'EOF'
basicConstraints = critical,CA:FALSE
keyUsage = critical,digitalSignature,keyEncipherment
extendedKeyUsage = serverAuth
subjectAltName = DNS:idp.localhost,DNS:proxy.localhost
subjectKeyIdentifier = hash
authorityKeyIdentifier = keyid,issuer
EOF

openssl ecparam -name prime256v1 -genkey -noout -out "$work/server.key"
openssl req -new -sha256 \
  -key "$work/server.key" \
  -subj "/CN=idp.localhost" \
  -out "$work/server.csr"
serial="0x$(openssl rand -hex 16)"
openssl x509 -req -sha256 \
  -in "$work/server.csr" \
  -CA "$intermediate" \
  -CAkey "$intermediate_key" \
  -set_serial "$serial" \
  -days 30 \
  -extfile "$work/extensions.cnf" \
  -out "$work/leaf.crt" >/dev/null 2>&1
cat "$work/leaf.crt" "$intermediate" >"$work/server.crt"

openssl verify -CAfile "$root" -untrusted "$intermediate" "$work/leaf.crt" >/dev/null
openssl x509 -in "$work/leaf.crt" -noout -checkhost idp.localhost >/dev/null
openssl x509 -in "$work/leaf.crt" -noout -checkhost proxy.localhost >/dev/null

chown 1000:1000 "$work/server.key" "$work/server.crt"
chmod 0400 "$work/server.key"
chmod 0444 "$work/server.crt"
mv -f "$work/server.key" "$private_key"
mv -f "$work/server.crt" "$certificate"
