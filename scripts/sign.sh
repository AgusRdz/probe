#!/bin/sh
# Sign a release binary with the Ed25519 private key.
#
# Usage: sh scripts/sign.sh <binary>
# Output: <binary>.sig  (base64-encoded signature)
#
# In GitHub Actions, signing.pem is written from the PROBE_SIGNING_KEY secret
# before this script is called, then deleted after.

set -e

BINARY="$1"

if [ -z "$BINARY" ]; then
  echo "usage: sign.sh <binary>" >&2
  exit 1
fi

if [ ! -f "$BINARY" ]; then
  echo "sign.sh: file not found: $BINARY" >&2
  exit 1
fi

if [ ! -f signing.pem ]; then
  echo "sign.sh: signing.pem not found" >&2
  exit 1
fi

openssl pkeyutl -sign -inkey signing.pem -in "$BINARY" | base64 > "${BINARY}.sig"
echo "Signed: ${BINARY}.sig"
