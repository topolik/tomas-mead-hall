#!/bin/sh
set -e

DIR="$(cd "$(dirname "$0")" && pwd)"
CA_KEY="$DIR/ca.key"
CA_CERT="$DIR/ca.crt"
SRV_KEY="$DIR/server.key"
SRV_CERT="$DIR/server.crt"

if [ -f "$CA_CERT" ] && [ -f "$SRV_CERT" ]; then
    echo "Certificates already exist. Delete them to regenerate."
    exit 0
fi

# Generate CA
openssl genrsa -out "$CA_KEY" 2048
openssl req -x509 -new -nodes -key "$CA_KEY" -sha256 -days 3650 \
    -subj "/CN=DSH Local CA" -out "$CA_CERT"

# Generate server cert for meshnet IP
openssl genrsa -out "$SRV_KEY" 2048
openssl req -new -key "$SRV_KEY" -subj "/CN=DSH Dashboard" \
    -addext "subjectAltName=IP:100.x.y.z,IP:127.0.0.1,DNS:localhost" \
    -out "$DIR/server.csr"
openssl x509 -req -in "$DIR/server.csr" -CA "$CA_CERT" -CAkey "$CA_KEY" \
    -CAcreateserial -days 3650 -sha256 \
    -extfile <(printf "subjectAltName=IP:100.x.y.z,IP:127.0.0.1,DNS:localhost") \
    -out "$SRV_CERT"
rm -f "$DIR/server.csr" "$DIR/ca.srl"

echo "Generated:"
echo "  CA cert:     $CA_CERT  (install on phone)"
echo "  Server cert: $SRV_CERT"
echo "  Server key:  $SRV_KEY"
