#!/bin/bash

# Installation script for time-leak systemd service

set -e

PROJECT_PATH="/home/time-leak-back-end"
SERVICE_FILE="time-leak.service"
SYSTEM_SERVICE_PATH="/etc/systemd/system/$SERVICE_FILE"

echo "🔧 Installing time-leak systemd service..."

# Check if running as root
if [ "$EUID" -ne 0 ]; then 
    echo "❌ This script must be run as root. Use: sudo bash install-service.sh"
    exit 1
fi

# Build the binary (optional - if Go is installed)
if command -v go &> /dev/null; then
    echo "📦 Building the Go binary..."
    cd "$PROJECT_PATH"
    go mod tidy
    go build -o bin/time-leak ./cmd
    echo "✅ Binary built successfully at: $PROJECT_PATH/bin/time-leak"
else
    echo "⚠️  Go is not installed. The service will use 'go run' instead."
    echo "   Install Go from https://golang.org/dl/ for production use."
fi

# Create data directory if it doesn't exist
mkdir -p "$PROJECT_PATH/data"

# Copy service file to systemd
echo "📋 Installing service file to $SYSTEM_SERVICE_PATH..."
cp "$PROJECT_PATH/$SERVICE_FILE" "$SYSTEM_SERVICE_PATH"

# Reload systemd daemon
echo "🔄 Reloading systemd daemon..."
systemctl daemon-reload

# Enable the service to start on boot
echo "⚡ Enabling service on boot..."
systemctl enable $SERVICE_FILE

# Start the service
echo "▶️  Starting service..."
systemctl start $SERVICE_FILE

# Check service status
echo ""
echo "📊 Service Status:"
systemctl status $SERVICE_FILE --no-pager

echo ""
echo "✅ Installation complete!"
echo ""
echo "Useful commands:"
echo "  - Start service:    sudo systemctl start time-leak"
echo "  - Stop service:     sudo systemctl stop time-leak"
echo "  - Restart service:  sudo systemctl restart time-leak"
echo "  - Check status:     sudo systemctl status time-leak"
echo "  - View logs:        sudo journalctl -u time-leak -f"
echo "  - Disable on boot:  sudo systemctl disable time-leak"
