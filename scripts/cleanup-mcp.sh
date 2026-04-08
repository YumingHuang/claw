#!/usr/bin/env bash
# scripts/cleanup-mcp.sh — Kill orphaned MCP child processes (headless Chrome, Node.js, etc.)
# Run this after a crash or unclean shutdown of claw.
set -euo pipefail

echo "Looking for orphaned MCP processes..."

killed=0

# Kill orphaned Playwright/Chrome processes spawned by npx
for pattern in "playwright" "chromium" "chrome" "node.*@playwright" "node.*@modelcontextprotocol"; do
    pids=$(pgrep -f "$pattern" 2>/dev/null || true)
    for pid in $pids; do
        # Skip if the process has a living claw parent
        ppid=$(ps -o ppid= -p "$pid" 2>/dev/null | tr -d ' ')
        if [ -n "$ppid" ] && [ "$ppid" != "1" ] && ps -p "$ppid" -o comm= 2>/dev/null | grep -q "claw"; then
            continue
        fi
        echo "  Killing orphaned process: pid=$pid $(ps -o command= -p "$pid" 2>/dev/null | head -c 80)"
        kill -TERM "$pid" 2>/dev/null || true
        killed=$((killed + 1))
    done
done

if [ "$killed" -eq 0 ]; then
    echo "No orphaned MCP processes found."
else
    echo "Killed $killed orphaned process(es)."
    # Give them a moment, then force-kill any survivors
    sleep 2
    for pattern in "playwright" "chromium" "chrome"; do
        pids=$(pgrep -f "$pattern" 2>/dev/null || true)
        for pid in $pids; do
            ppid=$(ps -o ppid= -p "$pid" 2>/dev/null | tr -d ' ')
            if [ -n "$ppid" ] && [ "$ppid" = "1" ]; then
                echo "  Force-killing: pid=$pid"
                kill -9 "$pid" 2>/dev/null || true
            fi
        done
    done
fi
