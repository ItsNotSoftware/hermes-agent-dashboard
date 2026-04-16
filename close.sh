#!/bin/bash
# Kill chromium dashboard window
pkill -f "localhost:9200" 2>/dev/null
echo "Dashboard closed"
