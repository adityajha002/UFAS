#!/bin/sh
set -e

PORT="${PORT:-8080}"
echo "Starting UFAS on Render on port $PORT..."

# Replace listen port dynamically with Render's assigned $PORT
sed -i "s/listen: \".*\"/listen: \"0.0.0.0:$PORT\"/" /etc/ufas/config.server.yaml

# Start admin-server in the background on 127.0.0.1:9090
admin-server --config /etc/ufas/config.admin.yaml serve &

# Wait briefly for admin-server to bind
sleep 1

# Start main attendance-api in foreground on Render's $PORT
exec server --config /etc/ufas/config.server.yaml serve
