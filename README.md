# Dufs MCP Server

基于 dufs API 封装的 MCP (Model Context Protocol) Server，通过 SSE (Server-Sent Events) 提供文件操作服务。

## 功能特性

- ✅ 文件上传
- ✅ 文件下载
- ✅ 文件/目录删除
- ✅ 目录列表和搜索
- ✅ 创建目录
- ✅ 移动/重命名文件
- ✅ 获取文件哈希值
- ✅ 下载文件夹为 zip
- ✅ 健康检查
- ✅ 支持 Basic Auth 认证
- ✅ 通过配置文件或环境变量配置

## 安装

### 编译

```bash
go build -o dufs-mcp-server main.go
```

### 直接运行

```bash
go run main.go
```

## 配置

所有配置通过环境变量传入，由 MCP 客户端在启动时设置：

```bash
export DUFS_URL="http://127.0.0.1:5000"
export DUFS_USERNAME="admin"
export DUFS_PASSWORD="password"
export DUFS_UPLOAD_DIR="/uploads"
export DUFS_ALLOW_INSECURE="false"
export MCP_MODE="stdio"  # 或 "http"/"sse"
export PORT="7887"       # 仅在 HTTP 模式下使用
```

### 必需的环境变量

- `DUFS_URL`: dufs 服务器的地址（必需）

### 可选的环境变量

- `DUFS_USERNAME`: 用户名（如果 dufs 需要认证）
- `DUFS_PASSWORD`: 密码（如果 dufs 需要认证）
- `DUFS_UPLOAD_DIR`: 默认上传目录
- `DUFS_ALLOW_INSECURE`: 是否允许不安全的连接（true/false）
- `MCP_MODE`: 运行模式，可选值：
  - `stdio` (默认): 标准 MCP 协议，通过 stdin/stdout 通信
  - `http` 或 `sse`: HTTP/SSE 模式，通过 HTTP 端点通信
- `PORT`: MCP server 监听端口（仅在 HTTP 模式下使用，默认 7887）

## 运行模式

### 模式 1: stdio 模式（标准 MCP 协议，推荐）

这是标准的 MCP server 运行方式，通过 stdin/stdout 进行 JSON-RPC 通信。这是默认模式，也是 MCP 客户端推荐的方式。

```bash
# 直接运行（stdio 模式）
DUFS_URL=http://127.0.0.1:5000 DUFS_USERNAME=admin DUFS_PASSWORD=pass ./dufs-mcp-server

# 或者明确指定模式
MCP_MODE=stdio DUFS_URL=http://127.0.0.1:5000 ./dufs-mcp-server
```

### 模式 2: HTTP/SSE 模式

通过 HTTP 端点提供服务，支持 SSE（Server-Sent Events）和 REST API。

```bash
# HTTP 模式
MCP_MODE=http DUFS_URL=http://127.0.0.1:5000 PORT=7887 ./dufs-mcp-server
```

## API 端点

### SSE 端点

- `GET /sse` - Server-Sent Events 端点，用于 MCP 协议通信

### HTTP 端点

- `POST /message` - 直接发送 JSON-RPC 消息（用于测试）

## MCP 工具

### 1. dufs_upload_batch

批量上传文件并立即返回 `job_id`，上传任务在后台异步执行。即使只上传单个文件也推荐使用该工具（传入一个文件即可），可以避免前端等待造成的超时。

**特性**：
- 支持一个或多个文件，`files` 数组中的每一项都包含 `local_path`，可选 `remote_path`
- 如果未指定 `remote_path`，自动使用配置的 `upload_dir`（默认为 `uploads`）+ 当日目录（`YYYYMMDD`）+ 文件名
- 自动创建所需的远程目录结构
- 工具调用会在 1 秒内返回 `job_id` 与任务状态，不会阻塞 Cursor

```json
{
  "name": "dufs_upload_batch",
  "arguments": {
    "files": [
      {
        "local_path": "/path/to/a.zip",
        "remote_path": "dufs-mcp-server/a.zip"
      },
      {
        "local_path": "/path/to/b.zip"
      }
    ]
  }
}
```

返回示例：

```json
{
  "success": true,
  "job_id": "job-1732532145123456789",
  "status": "pending",
  "task_count": 2
}
```

单文件上传时同样把 `files` 数组设置为一个元素即可。

### 2. dufs_upload_status

查询批量上传任务的状态与每个文件的执行结果，便于在任务完成后获取远程路径、HTTP 状态码以及错误详情。

```json
{
  "name": "dufs_upload_status",
  "arguments": {
    "job_id": "job-1732532145123456789"
  }
}
```

返回数据包含整体状态（`pending` / `running` / `completed` / `failed`）以及每个文件的上传结果、耗时、错误信息等，适合在批量上传后再查询目录结构或结果。

### 2. dufs_download

从 dufs 服务器下载文件

```json
{
  "name": "dufs_download",
  "arguments": {
    "remote_path": "/uploads/file.txt",
    "local_path": "/path/to/save/file.txt"
  }
}
```

### 3. dufs_delete

删除文件或目录

```json
{
  "name": "dufs_delete",
  "arguments": {
    "path": "/uploads/file.txt"
  }
}
```

### 4. dufs_list

列出目录内容

```json
{
  "name": "dufs_list",
  "arguments": {
    "path": "/",
    "query": "*.txt",
    "format": "json"
  }
}
```

### 5. dufs_create_dir

创建目录

```json
{
  "name": "dufs_create_dir",
  "arguments": {
    "path": "/new-folder"
  }
}
```

### 6. dufs_move

移动或重命名文件/目录

```json
{
  "name": "dufs_move",
  "arguments": {
    "source": "/old-path/file.txt",
    "destination": "/new-path/file.txt"
  }
}
```

### 7. dufs_get_hash

获取文件的 SHA256 哈希值

```json
{
  "name": "dufs_get_hash",
  "arguments": {
    "path": "/uploads/file.txt"
  }
}
```

### 8. dufs_download_folder

下载整个文件夹为 zip

```json
{
  "name": "dufs_download_folder",
  "arguments": {
    "remote_path": "/uploads/folder",
    "local_path": "/path/to/save/folder.zip"
  }
}
```

### 9. dufs_health

检查 dufs 服务器健康状态

```json
{
  "name": "dufs_health",
  "arguments": {}
}
```

## 使用示例

### 使用 curl 测试

```bash
# 发送初始化请求
curl -X POST http://localhost:8080/message \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "initialize",
    "params": {}
  }'

# 列出工具
curl -X POST http://localhost:8080/message \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "id": 2,
    "method": "tools/list",
    "params": {}
  }'

# 调用上传工具
curl -X POST http://localhost:8080/message \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "id": 3,
    "method": "tools/call",
    "params": {
      "name": "dufs_upload",
      "arguments": {
        "local_path": "/tmp/test.txt"
      }
    }
  }'
```

## MCP 客户端配置

### stdio 模式配置（推荐）

在 MCP 客户端（如 Cursor）中配置，使用标准的 stdio 模式：

```json
{
  "mcpServers": {
    "dufs": {
      "command": "./dufs-mcp-server",
      "env": {
        "DUFS_URL": "http://127.0.0.1:5000",
        "DUFS_USERNAME": "admin",
        "DUFS_PASSWORD": "password",
        "DUFS_UPLOAD_DIR": "/uploads",
        "MCP_MODE": "stdio"
      }
    }
  }
}
```

**注意**：
- 不需要指定 `args` 或 API 端点
- 程序通过 stdin/stdout 与 MCP 客户端通信
- 所有参数通过 `env` 字段传入
- `MCP_MODE` 默认为 `stdio`，可以省略

### HTTP 模式配置（可选）

如果需要使用 HTTP 模式，可以这样配置：

```json
{
  "mcpServers": {
    "dufs": {
      "command": "./dufs-mcp-server",
      "env": {
        "DUFS_URL": "http://127.0.0.1:5000",
        "DUFS_USERNAME": "admin",
        "DUFS_PASSWORD": "password",
        "MCP_MODE": "http",
        "PORT": "7887"
      }
    }
  }
}
```

然后通过 HTTP 端点访问：
- `POST http://localhost:7887/message` - 发送 JSON-RPC 消息
- `GET http://localhost:7887/sse` - SSE 端点

## 依赖

- Go 1.21+ （仅使用标准库，无外部依赖）

## 许可证

MIT

