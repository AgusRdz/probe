#!/bin/sh
# Generate an Ed25519 keypair for probe release signing.
#
# Outputs:
#   signing.pem         — private key (KEEP SECRET, add to GitHub Actions secret PROBE_SIGNING_KEY)
#   updater/public_key.pem — public key (commit to repo, embedded in binary)
#
# Usage: sh scripts/keygen.sh

set -e

if [ -f signing.pem ]; then
  echo "signing.pem already exists. Remove it first if you want to rotate keys." >&2
  exit 1
fi

openssl genpkey -algorithm ed25519 -out signing.pem
openssl pkey -in signing.pem -pubout -out updater/public_key.pem

echo "Generated:"
echo "  signing.pem            (private — do NOT commit)"
echo "  updater/public_key.pem (public  — commit this file)"
echo ""
echo "Add the signing key to GitHub Actions as secret PROBE_SIGNING_KEY:"
echo "  base64 signing.pem | tr -d '\\n' | pbcopy   # macOS"
echo "  base64 -w0 signing.pem                       # Linux (copy output)"
