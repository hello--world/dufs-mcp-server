# Dufs MCP Server

这是一个 **Dufs 的 MCP 文件操作适配器**，不是 Dufs 本体，也不是 CI 制品上传服务。

## 职责边界

- **Dufs**：负责文件存储、HTTP `PUT` 上传、`GET` 下载、认证和目录 ACL。
- **CI / 自托管 Runner**：直接通过 Dufs 的 HTTP API 上传文件，使用项目专属凭据；MCP/AI 不参与，也不依赖其运行状态。
- **本项目（MCP）**：向智能体提供受限制的目录查询、哈希检查、本地下载等工具。只有显式授权时才允许上传、移动或删除。
- **外部下载与 Caddy**：由部署者配置下载域名、认证、TLS 及只读路由。本项目不部署 Caddy，也不提供额外上传网关。

因此，即使本项目或 AI 客户端停止，已部署的 Dufs 仍可独立提供 CI 上传和下载。

## 构建和启动

要求 Go 1.25.4+。必须以 **整个模块** 编译，不能使用旧命令 `go build main.go`。

```bash
go build -o dufs-mcp-server .
go test ./... -race
```

通过具有受限 Dufs 读权限的专用账号运行：

```bash
export DUFS_URL="https://dufs.internal.example"
export DUFS_USERNAME="mcp_reader"
export DUFS_PASSWORD="替换为服务端专用密码"
export DUFS_REMOTE_ROOT="projects/apios"
export DUFS_LOCAL_ROOT="/srv/mcp-exchange"  # 须预先创建并限制操作系统权限
export MCP_MODE="stdio"
./dufs-mcp-server
```

本地调试可使用 `DUFS_URL=http://127.0.0.1:5000`。指向其他主机的明文 HTTP 默认拒绝，确为受保护的内网时必须显式设置 `DUFS_ALLOW_INSECURE=true`，不建议在公网使用。

## 权限和安全参数

| 参数 | 默认值 | 作用 |
| --- | --- | --- |
| `DUFS_URL` | 必填 | 上游 Dufs 的 HTTP(S) 地址 |
| `DUFS_USERNAME`、`DUFS_PASSWORD` | 空 | 用于 Dufs 的专用账号；应仅授予需要的路径权限 |
| `DUFS_REMOTE_ROOT` | `DUFS_UPLOAD_DIR` 或 `uploads` | 本 MCP 允许访问的远端目录根；禁止跨根操作 |
| `DUFS_LOCAL_ROOT` | 未设置 | 允许读写的本地目录根；本地下载/上传必须先配置 |
| `DUFS_MCP_ENABLE_UPLOAD` | `false` | 允许向远端指定根上传和创建目录 |
| `DUFS_MCP_ENABLE_DELETE` | `false` | 允许在远端指定根删除 |
| `DUFS_MCP_ENABLE_MOVE` | `false` | 允许在远端指定根移动文件 |
| `DUFS_ALLOW_INSECURE` | `false` | 允许非本机地址使用 HTTP 明文 |
| `MCP_MODE` | `stdio` | 推荐使用标准 MCP stdio |

默认提供：`dufs_health`、`dufs_list`、`dufs_get_hash`、`dufs_download`、`dufs_download_folder`。其中下载会在本地目录内**仅创建新文件**，不会覆盖已有文件；未设置 `DUFS_LOCAL_ROOT` 时下载返回明确错误。

要让智能体上传，必须同时满足：其 MCP 进程设置 `DUFS_MCP_ENABLE_UPLOAD=true`、具备受限的 `DUFS_LOCAL_ROOT`，并使用 Dufs 上只对指定项目路径拥有读写权限的凭据。不应给智能体 CI 使用的跨项目写账号。

其它安全约束：

- 本地文件操作使用 Go `os.Root`，防止路径穿越和符号链接逃逸；远端拒绝 `..`、重复分隔符及可疑 URL 编码。
- 单次文件传输上限 1 GiB，单次批量上传最多 32 项；异步上传任务索引最多保留 128 项。
- 异步任务**只保存在内存**。MCP 进程重启后，旧任务状态不可恢复，不能仅根据上传请求返回 `job_id` 就认为完成。
- 不跟随 Dufs HTTP 重定向；列表响应最多读取 2 MiB；下载异常会删除部分本地文件。
- 是否真正能读写由 **Dufs 自身 ACL 和操作权限**再次决定，MCP 路径范围只提供额外保护。
- `dufs_get_hash` 需要 Dufs 服务开启 `--allow-hash`；文件夹归档需要 `--allow-archive`。没有开启时应明确失败，而不是返回成功。

## MCP 工具

| 工具 | 功能 | 默认 |
| --- | --- | --- |
| `dufs_list` | 列表和名称搜索，`path` 不填则使用远端根 | 可用 |
| `dufs_get_hash` | 查询指定文件 SHA-256 | 可用 |
| `dufs_download` | 下载到本地授权目录 | 可用，需本地目录 |
| `dufs_download_folder` | 下载文件夹 zip | 可用，需本地目录与上游归档许可 |
| `dufs_health` | 上游健康状态 | 可用 |
| `dufs_upload` | 单文件同步上传，可选 `async=true` | 关闭 |
| `dufs_upload_batch` | 批量上传，默认异步 | 关闭 |
| `dufs_upload_status` | 查询进程内任务状态 | 可用 |
| `dufs_create_dir` | 创建目录 | 关闭 |
| `dufs_move` | 移动 / 重命名 | 关闭 |
| `dufs_delete` | 删除 | 关闭 |

示例工具参数：

```json
{"name":"dufs_list","arguments":{"path":"projects/apios","format":"json"}}
```

```json
{"name":"dufs_download","arguments":{"remote_path":"projects/apios/build/log.txt","local_path":"log.txt"}}
```

第二个示例将写入 `DUFS_LOCAL_ROOT/log.txt`，文件已存在则拒绝覆盖。上传时 `local_path` 同样受本地目录根约束。

## HTTP 调试模式与限制

推荐用 **stdio** 对接 MCP 客户端。旧 `/message` 是仅供受控调试的 JSON-RPC HTTP 端点，**不是完整的 MCP Streamable HTTP / SSE 实现**。原来的 `/sse` 仅有空心连接，现明确返回 `501`，避免客户端误认为可用。

如确实需要 HTTP 调试：

```bash
MCP_MODE=http MCP_HTTP_BIND=127.0.0.1:7887 \
  MCP_HTTP_TOKEN="请使用至少32字节的随机令牌" ./dufs-mcp-server
```

`POST /message` 必须带 `Authorization: Bearer <token>`；默认只绑定回环地址，拒绝浏览器 `Origin` 请求、匿名调用及无上限消息体。远程监听仅供已有认证/TLS 反向代理场景，必须显式启用 `MCP_HTTP_ALLOW_REMOTE=true`，不应直接暴露公网。

## 快速上手：CI 直传 Dufs（不启动 MCP）

1. **单独部署 Dufs 服务本体**（本仓库只有 MCP 适配器，不包含 Dufs 服务）。在 Dufs 上预创建 `/projects/apios` 目录，并配置项目独立账号。例如只在开发机回环接口验证的示意命令：

```bash
# 仅本机示意：正式环境应将凭据移到受保护的配置，
# 跨机器连接使用内网 HTTPS/TLS，不应公开写接口。
dufs /srv/dufs-data -b 127.0.0.1 -p 5000 \
  --allow-upload --allow-hash --allow-search \
  -a 'ci_apios:REPLACE_CI_PASSWORD@/projects/apios:rw' \
  -a 'mcp_reader:REPLACE_MCP_PASSWORD@/projects/apios:ro'
```

2. **自托管 Runner 直接 HTTP PUT**，不需要 MCP 或 AI。下面是单个构建文件的示例；使用实际内网 Dufs URL 和专属 CI 账号，将路径中的日期、运行号和随机 ID 由工作流生成：

```bash
: "${DUFS_INTERNAL_URL:?}" "${DUFS_CI_USER:?}" "${DUFS_CI_PASSWORD:?}"
: "${GITHUB_RUN_ID:?}" "${GITHUB_RUN_ATTEMPT:?}"
test -f dist/package.zip
ARTIFACT_ID="$(od -An -N16 -tx1 /dev/urandom | tr -d ' \n')"
REMOTE="projects/apios/$(date -u +%Y%m%d)/${GITHUB_RUN_ID}/${GITHUB_RUN_ATTEMPT}/${ARTIFACT_ID}/package.zip"
curl --fail-with-body --show-error --silent \
  --user "${DUFS_CI_USER}:${DUFS_CI_PASSWORD}" \
  --upload-file dist/package.zip "${DUFS_INTERNAL_URL%/}/${REMOTE}"
```

3. **上传后要验证真实远端数据**（Dufs 需启用 `--allow-hash`）；不能仅以 HTTP PUT 2xx 判定归档成功：

```bash
LOCAL_SHA="$(sha256sum dist/package.zip | awk '{print $1}')"
REMOTE_SHA="$(curl --fail-with-body --show-error --silent \
  --user "${DUFS_CI_USER}:${DUFS_CI_PASSWORD}" \
  "${DUFS_INTERNAL_URL%/}/${REMOTE}?hash" | tr -d '\r\n')"
test "${LOCAL_SHA}" = "${REMOTE_SHA}" || { echo '远端哈希不一致' >&2; exit 1; }
printf '验证通过: %s %s\n' "${REMOTE}" "${LOCAL_SHA}"
```

外网下载必须单独在 Caddy 配置只读路由、身份验证及 TLS，且禁止匿名下载、目录列表和写入请求。**本 PR 没有部署 Dufs/Caddy，也没有修改 APIOS PR #59 的工作流**。上面只是一段首次联调命令；接入 APIOS 时仍应保留其已有的本机归档、清单、SHA-256 校验和失败阻断逻辑。

## 下载已上传的 CI 制品

CI 上传使用的远端文件路径（`REMOTE`）必须写入归档清单；仅有运行编号不足以猜出随机 ID。以下示例使用某次上传后的完整路径：

```bash
REMOTE="projects/apios/20261115/123456/1/a8f39c2d/package.zip"
```

### 方式一：内网直接从 Dufs 下载

在能访问 Dufs 内网地址的机器上，使用**只读账号**通过 GET 下载：

```bash
: "${DUFS_INTERNAL_URL:?}" "${DUFS_READ_USER:?}" "${DUFS_READ_PASSWORD:?}"
curl --fail-with-body --show-error --silent \
  --user "${DUFS_READ_USER}:${DUFS_READ_PASSWORD}" \
  --output package.zip \
  "${DUFS_INTERNAL_URL%/}/${REMOTE}"
```

示例环境：Dufs 本机可设置 `DUFS_INTERNAL_URL=http://127.0.0.1:5000`；远端 Runner 必须使用它实际能连通的内网 URL，**不能**指向 Runner 自己的 127.0.0.1。

### 方式二：外网通过 Caddy 的认证下载域名

正式部署时，Caddy 接收外网 HTTPS 请求并要求用户认证，然后用**单独的只读上游身份**向 Dufs 读取文件。外部下载 URI 与 Dufs 相同，只有主机名不同，例如：

```text
https://files.example.com/projects/apios/20261115/123456/1/a8f39c2d/package.zip
```

浏览器打开链接完成登录后即可下载；命令行客户端应按实际部署的认证机制传入凭据。若采用 Caddy Basic Auth：

```bash
: "${DUFS_PUBLIC_URL:?}" "${DOWNLOAD_USER:?}" "${DOWNLOAD_PASSWORD:?}"
curl --fail-with-body --show-error --silent \
  --user "${DOWNLOAD_USER}:${DOWNLOAD_PASSWORD}" \
  --output package.zip \
  "${DUFS_PUBLIC_URL%/}/${REMOTE}"
```

**Caddy 必须先部署好才能使用此链接**：只放行认证后的制品文件 `GET` / `HEAD`，拒绝全部写入方法、目录访问及目录搜索/列表 URL，强制 HTTPS。若 Caddy 和 Dufs 使用不同的凭据体系，代理必须使用专属 Dufs 只读上游凭据，不应直接转发具有写权限的外部身份。不能把随机文件名当作认证。

### 验证下载文件

将下载后的 `package.zip` 与 CI 归档清单记录的原始 SHA-256 对比，不能仅凭收到 HTTP 200 就认为校验通过：

```bash
: "${EXPECTED_SHA256:?请从 CI 制品清单读取原始哈希}"
test "$(sha256sum package.zip | awk '{print $1}')" = "${EXPECTED_SHA256}" \
  || { echo '下载的制品哈希校验失败' >&2; exit 1; }
echo "下载与 SHA-256 验证成功"
```

上例的 URL、账号、凭据、路径与哈希均为占位示例；**本 PR 尚未配置外网域名，也没有实际部署 Dufs/Caddy 或执行外网下载验收**。

## 快速上手：智能体使用 MCP

```bash
git clone https://github.com/buildHelpers/dufs-mcp-server.git
cd dufs-mcp-server
git switch fix/mcp-security-20261115
go build -o dufs-mcp-server .
mkdir -p "$HOME/dufs-mcp-exchange"
```

在 MCP 客户端的 stdio Server 配置中，设置如下环境变量（由客户端启动可执行文件，**不是打开 Web 网页**）：

```json
{
  "mcpServers": {
    "dufs": {
      "command": "/absolute/path/to/dufs-mcp-server",
      "env": {
        "MCP_MODE": "stdio",
        "DUFS_URL": "https://dufs.internal.example",
        "DUFS_USERNAME": "mcp_reader",
        "DUFS_PASSWORD": "REPLACE_MCP_PASSWORD",
        "DUFS_REMOTE_ROOT": "projects/apios",
        "DUFS_LOCAL_ROOT": "/absolute/path/to/dufs-mcp-exchange"
      }
    }
  }
}
```

`command` 必须改成真实绝对路径，`DUFS_LOCAL_ROOT` 必须是已存在的目录；真实凭据应由 MCP 客户端的安全配置或凭据管理提供，不要提交到 GitHub。默认允许查询、哈希检查和向本地受限目录下载，**不允许上传、删除或移动**。MCP 工具错误不会影响 CI 直传 Dufs。

## CI、Dufs 与验收现状

CI 应使用 Dufs 官方提供的 `PUT` / `GET` API（参考 [Dufs API](https://github.com/sigoden/dufs#api)），使用项目专属 ACL，落地制品仍需按原始 SHA-256 回读验证。APIOS 的本机归档/清单逻辑由其 PR #59 维护；本仓库不接管 CI 上传工作流，不生成 Caddy 配置，也不宣称已经完成外网验收。

本仓库的自托管 Linux 测试应运行 `go test ./... -race` 与 `go vet ./...`。伪造上游的单元测试不等于真实 Dufs 实机验收；正式启用前仍需按实际版本验证只读 ACL、下载、拒权与上游参数。

## 许可证

MIT
