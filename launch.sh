#!/bin/bash
echo "Waiting for dashboard server..."
for i in $(seq 1 10); do
    if curl -sf http://localhost:9200/api/status > /dev/null 2>&1; then
        break
    fi
    sleep 1
done
chromium --kiosk --no-first-run --disable-infobars --disable-session-crashed-bubble --disable-translate --no-default-browser-check --password-store=basic --force-device-scale-factor=1.0 --window-size=800,480 http://localhost:9200 2>/dev/null &
