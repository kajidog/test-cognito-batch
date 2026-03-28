#!/bin/sh
set -e

echo "Waiting for Garage to start..."
sleep 3

# Get node ID and configure layout
NODE_ID=$(garage -c /config/garage.toml node id -q | cut -c1-16)
garage -c /config/garage.toml layout assign -z dc1 -c 1G "$NODE_ID" >/dev/null 2>&1 || true
garage -c /config/garage.toml layout apply --version 1 >/dev/null 2>&1 || true

# Create API key if missing
if ! garage -c /config/garage.toml key info cognito-app-key >/dev/null 2>&1; then
  garage -c /config/garage.toml key create cognito-app-key >/dev/null
fi

# Create bucket and grant access
garage -c /config/garage.toml bucket create cognito-csv >/dev/null 2>&1 || true
garage -c /config/garage.toml bucket allow --read --write --owner cognito-csv --key cognito-app-key >/dev/null 2>&1 || true

KEY_INFO=$(garage -c /config/garage.toml key info cognito-app-key --show-secret)
ACCESS_KEY=$(printf '%s\n' "$KEY_INFO" | awk -F':' '/Key ID/ {gsub(/^[ \t]+/, "", $2); print $2; exit}')
SECRET_KEY=$(printf '%s\n' "$KEY_INFO" | awk -F':' '/Secret key/ {gsub(/^[ \t]+/, "", $2); print $2; exit}')

cat > /config/credentials.env <<EOF
AWS_ACCESS_KEY_ID=$ACCESS_KEY
AWS_SECRET_ACCESS_KEY=$SECRET_KEY
EOF

echo "Garage credentials written to /config/credentials.env"
