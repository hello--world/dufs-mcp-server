#!/usr/bin/env bash
set -euo pipefail

# 真实 Dufs + MCP stdio 冒烟检查；此脚本不启动任何可公开访问的 HTTP 服务。
: "${DUFS_URL:?请先设置实际 Dufs 地址}"
: "${DUFS_USERNAME:?请使用已授权的 Dufs 只读账号}"
: "${DUFS_PASSWORD:?请设置账号密码}"

{
  printf '%s\n' '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}'
  printf '%s\n' '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}'
  printf '%s\n' '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"dufs_health","arguments":{}}}'
} | MCP_MODE=stdio ./dufs-mcp-server | python3 -c '
import json,sys
messages=[json.loads(line) for line in sys.stdin if line.strip()]
if len(messages)!=3 or any("error" in m for m in messages):
    raise SystemExit("MCP initialize/tools/list/health failed")
health=json.loads(messages[2]["result"]["content"][0]["text"])
if not health.get("healthy"):
    raise SystemExit("Dufs health check failed: "+str(health))
print("REAL stdio MCP + Dufs health check passed")
'
