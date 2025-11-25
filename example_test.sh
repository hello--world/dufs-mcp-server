#!/bin/bash

# Dufs MCP Server 测试脚本

# 设置配置
export DUFS_URL="http://127.0.0.1:5000"
export DUFS_USERNAME="admin"
export DUFS_PASSWORD="password"
export PORT="8080"

# 启动服务器（后台运行）
./dufs-mcp-server &
SERVER_PID=$!

# 等待服务器启动
sleep 2

echo "=== 测试 MCP Server ==="
echo ""

# 1. 测试初始化
echo "1. 测试初始化..."
curl -X POST http://localhost:8080/message \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "initialize",
    "params": {}
  }' | jq .
echo ""

# 2. 列出工具
echo "2. 列出可用工具..."
curl -X POST http://localhost:8080/message \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "id": 2,
    "method": "tools/list",
    "params": {}
  }' | jq .
echo ""

# 3. 健康检查
echo "3. 健康检查..."
curl -X POST http://localhost:8080/message \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "id": 3,
    "method": "tools/call",
    "params": {
      "name": "dufs_health",
      "arguments": {}
    }
  }' | jq .
echo ""

# 4. 列出目录
echo "4. 列出根目录..."
curl -X POST http://localhost:8080/message \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "id": 4,
    "method": "tools/call",
    "params": {
      "name": "dufs_list",
      "arguments": {
        "path": "/",
        "format": "json"
      }
    }
  }' | jq .
echo ""

# 清理
kill $SERVER_PID 2>/dev/null
echo "测试完成！"

