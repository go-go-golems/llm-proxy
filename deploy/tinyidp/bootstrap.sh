#!/bin/sh
set -eu
umask 077

DB=/state/tinyidp.sqlite
MARKER=/state/.phase1-bootstrap-complete
TOKEN_SECRET=/state/token-secret

if [ ! -f "$MARKER" ]; then
  tinyidp admin --db "$DB" init --generate-signing-key --kid byok-initial
  dd if=/dev/urandom of="$TOKEN_SECRET" bs=32 count=1 status=none
  chmod 0600 "$TOKEN_SECRET"
  : >"$MARKER"
fi

if ! tinyidp admin --db "$DB" user get --login=owner@example.test >/dev/null 2>&1; then
  tinyidp admin --db "$DB" user create \
    --id=local-owner --sub=local-owner --login=owner@example.test \
    --email=owner@example.test --email-verified --name='Local Owner' \
    --password-from-stdin </run/secrets/tinyidp_bootstrap_password
fi

# The device client is public and limited to the agent resource and issuance
# scope. No client secret exists or is written.
if ! tinyidp admin --db "$DB" client get --id=llm-proxy-agent >/dev/null 2>&1; then
  tinyidp admin --db "$DB" client create \
    --id=llm-proxy-agent --public \
    --scope=openid --scope=profile --scope=email --scope=offline_access \
    --scope=llm.tokens.issue \
    --audience=https://proxy.localhost:18443/agent/v1 \
    --grant-type=urn:ietf:params:oauth:grant-type:device_code
fi

# The confidential resource client can introspect only the exact agent resource.
# It has no token grant type, and its operator secret never appears in argv,
# output, or logs.
if ! tinyidp admin --db "$DB" client get --id=llm-proxy-resource >/dev/null 2>&1; then
  tinyidp admin --db "$DB" client create \
    --id=llm-proxy-resource \
    --secret-file=/run/secrets/tinyidp_resource_client_secret \
    --can-introspect \
    --audience=https://proxy.localhost:18443/agent/v1
fi
