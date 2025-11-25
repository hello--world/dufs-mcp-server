#!/bin/bash

# 构建脚本，确保生成兼容 macOS 的二进制文件

set -e

echo "Building dufs-mcp-server..."

# 清理旧的构建
rm -f dufs-mcp-server

# 使用标准构建，确保包含所有必要的 load commands
go build -o dufs-mcp-server main.go

# 确保可执行
chmod +x dufs-mcp-server

# 验证架构
echo "Binary info:"
file dufs-mcp-server

# 测试运行（应该会报错缺少配置，但说明程序可以启动）
echo ""
echo "Testing binary (should show config error):"
DUFS_URL=test ./dufs-mcp-server 2>&1 | head -2 || true

echo ""
echo "Build complete: dufs-mcp-server"

