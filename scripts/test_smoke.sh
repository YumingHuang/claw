#!/bin/bash
set -e
BASE="${1:-http://localhost:8080}"

echo "=== Health Check ==="
curl -s "$BASE/health" | jq .

echo ""
echo "=== Simple Chat ==="
curl -s -X POST "$BASE/v1/chat" \
  -H "Content-Type: application/json" \
  -d '{"message": "你好，请告诉我现在的时间"}' | jq .

echo ""
echo "=== Stream Chat ==="
curl -N -X POST "$BASE/v1/chat" \
  -H "Content-Type: application/json" \
  -d '{"message": "Hello!", "stream": true}'

echo ""
echo "=== Status ==="
curl -s "$BASE/status" | jq .

echo ""
echo "=== Done ==="
