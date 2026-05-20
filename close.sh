#!/bin/bash
# Stop the Hermes Agent Dashboard (Go binary).
pkill -f "/dashboard$" 2>/dev/null
pkill -x dashboard 2>/dev/null
echo "Dashboard closed"
