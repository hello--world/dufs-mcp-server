# Release Notes

## v1.0.0

### 功能特性

- ✅ 文件上传（支持自动按日期创建目录）
- ✅ 文件下载
- ✅ 文件/目录删除
- ✅ 目录列表和搜索
- ✅ 创建目录
- ✅ 移动/重命名文件
- ✅ 获取文件哈希值
- ✅ 下载文件夹为 zip
- ✅ 健康检查
- ✅ 支持 Basic Auth 认证
- ✅ 支持 stdio 和 HTTP/SSE 两种模式
- ✅ 多平台支持（Linux, macOS, Windows）

### 使用方式

所有配置通过环境变量传入：

```bash
export DUFS_URL="http://127.0.0.1:5000"
export DUFS_USERNAME="admin"
export DUFS_PASSWORD="password"
export DUFS_UPLOAD_DIR="/uploads"
```

### MCP 客户端配置

```json
{
  "mcpServers": {
    "dufs": {
      "command": "./dufs-mcp-server",
      "env": {
        "DUFS_URL": "http://127.0.0.1:5000",
        "DUFS_USERNAME": "admin",
        "DUFS_PASSWORD": "password"
      }
    }
  }
}
```

