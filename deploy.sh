#!/usr/bin/env bash
set -euo pipefail

PROJECT_DIR="$(cd "$(dirname "$0")" && pwd)"
SECRET="${SECRET:-clienthub-secret-key}"
SERVER_HOST="a1"
SERVER_IP="8.146.210.7"
SERVER_REMOTE_DIR="/opt/clienthub"
LOCAL_BIN_DIR="$HOME/.local/bin"
LOCAL_CONFIG_DIR="$HOME/.config/clienthub"

LDFLAGS="-s -w"

echo "==> Building server (linux/amd64) ..."
cd "$PROJECT_DIR"
mkdir -p build
GOOS=linux GOARCH=amd64 go build -ldflags "$LDFLAGS" -o build/hub-server ./cmd/server

echo "==> Building client + hubctl (darwin/arm64) ..."
GOOS=darwin GOARCH=arm64 go build -ldflags "$LDFLAGS" -o build/hub-client ./cmd/client
GOOS=darwin GOARCH=arm64 go build -ldflags "$LDFLAGS" -o build/hubctl     ./cmd/manager

# --- Deploy server to a1 ---
echo ""
echo "==> Deploying server to ${SERVER_HOST} ..."

ssh "$SERVER_HOST" "mkdir -p ${SERVER_REMOTE_DIR}"
scp build/hub-server "${SERVER_HOST}:${SERVER_REMOTE_DIR}/hub-server"

ssh "$SERVER_HOST" "cat > ${SERVER_REMOTE_DIR}/server.yaml" <<EOF
listen_addr: ":7900"
udp_addr: ":7901"
admin_addr: ":7902"
secret: "${SECRET}"
log_level: "info"
EOF

ssh "$SERVER_HOST" "cat > /etc/systemd/system/clienthub.service" <<EOF
[Unit]
Description=ClientHub Port Forwarding Server
After=network.target

[Service]
Type=simple
ExecStart=${SERVER_REMOTE_DIR}/hub-server -config ${SERVER_REMOTE_DIR}/server.yaml
Restart=always
RestartSec=5
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
EOF

ssh "$SERVER_HOST" "systemctl daemon-reload && systemctl enable clienthub && systemctl restart clienthub"
echo "    Server deployed and running on ${SERVER_HOST}"
ssh "$SERVER_HOST" "sleep 1 && systemctl status clienthub --no-pager -l" || true

# --- Deploy client + hubctl locally ---
echo ""
echo "==> Installing client and hubctl locally ..."

mkdir -p "$LOCAL_BIN_DIR"
cp build/hub-client "$LOCAL_BIN_DIR/hub-client"
cp build/hubctl     "$LOCAL_BIN_DIR/hubctl"
chmod +x "$LOCAL_BIN_DIR/hub-client" "$LOCAL_BIN_DIR/hubctl"

mkdir -p "$LOCAL_CONFIG_DIR"
cat > "${LOCAL_CONFIG_DIR}/client.yaml" <<EOF
server_addr: "${SERVER_IP}:7900"
client_name: "$(hostname -s)"
secret: "${SECRET}"
log_level: "info"

expose: []
EOF

echo "    Installed: ${LOCAL_BIN_DIR}/hub-client"
echo "    Installed: ${LOCAL_BIN_DIR}/hubctl"
echo "    Config:    ${LOCAL_CONFIG_DIR}/client.yaml"

# --- Verify PATH ---
if ! echo "$PATH" | tr ':' '\n' | grep -qx "$LOCAL_BIN_DIR"; then
    echo ""
    echo "    NOTE: Add ${LOCAL_BIN_DIR} to your PATH:"
    echo "      echo 'export PATH=\"\$HOME/.local/bin:\$PATH\"' >> ~/.zshrc && source ~/.zshrc"
fi

echo ""
echo "============================================"
echo "  Deployment complete!"
echo "============================================"
echo ""
echo "Server (${SERVER_HOST}):"
echo "  ssh ${SERVER_HOST} systemctl status clienthub"
echo ""
echo "Local client:"
echo "  hub-client -config ${LOCAL_CONFIG_DIR}/client.yaml"
echo ""
echo "Management (if ports 7900-7902 open in cloud security group):"
echo "  hubctl -a ${SERVER_IP}:7902 -s '${SECRET}' status"
echo ""
echo "Management (via SSH tunnel, if ports blocked):"
echo "  ssh -L 7902:127.0.0.1:7902 -L 7900:127.0.0.1:7900 -N ${SERVER_HOST} &"
echo "  hubctl -a 127.0.0.1:7902 -s '${SECRET}' status"
echo "  hub-client -config ${LOCAL_CONFIG_DIR}/client.yaml  # edit server_addr to 127.0.0.1:7900"
echo ""
