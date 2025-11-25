package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

// MCP 协议消息结构
type MCPMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *MCPError       `json:"error,omitempty"`
}

type MCPError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// MCP 工具定义
type MCPTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

// 配置结构
type Config struct {
	DufsURL       string `json:"dufs_url"`
	Username      string `json:"username,omitempty"`
	Password      string `json:"password,omitempty"`
	UploadDir     string `json:"upload_dir,omitempty"`
	AllowInsecure bool   `json:"allow_insecure,omitempty"`
}

// DufsClient 封装 dufs API 调用
type DufsClient struct {
	BaseURL  string
	Username string
	Password string
	Client   *http.Client
}

func NewDufsClient(config Config) *DufsClient {
	return &DufsClient{
		BaseURL:  config.DufsURL,
		Username: config.Username,
		Password: config.Password,
		Client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *DufsClient) makeRequest(method, path string, body io.Reader, headers map[string]string) (*http.Response, error) {
	url := strings.TrimSuffix(c.BaseURL, "/") + "/" + strings.TrimPrefix(path, "/")
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}

	// 添加认证
	if c.Username != "" && c.Password != "" {
		req.SetBasicAuth(c.Username, c.Password)
	}

	// 添加自定义 headers
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	return c.Client.Do(req)
}

// MCPServer MCP 服务器
type MCPServer struct {
	dufsClient *DufsClient
	tools      []MCPTool
	config     Config
}

func NewMCPServer(config Config) *MCPServer {
	dufsClient := NewDufsClient(config)

	tools := []MCPTool{
		{
			Name:        "dufs_upload",
			Description: "上传文件到 dufs 服务器。如果未指定 remote_path，代码会自动创建路径：upload_dir/YYYYMMDD/文件名（例如：uploads/20251125/file.txt）",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"local_path": map[string]interface{}{
						"type":        "string",
						"description": "本地文件路径",
					},
					"remote_path": map[string]interface{}{
						"type":        "string",
						"description": "远程文件路径（可选）。如果未指定，代码会自动创建路径：配置的 upload_dir（默认为 uploads）/当前日期（YYYYMMDD格式）/文件名。例如：uploads/20251125/file.txt",
					},
				},
				"required": []string{"local_path"},
			},
		},
		{
			Name:        "dufs_download",
			Description: "从 dufs 服务器下载文件",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"remote_path": map[string]interface{}{
						"type":        "string",
						"description": "远程文件路径",
					},
					"local_path": map[string]interface{}{
						"type":        "string",
						"description": "本地保存路径（可选）",
					},
				},
				"required": []string{"remote_path"},
			},
		},
		{
			Name:        "dufs_delete",
			Description: "删除 dufs 服务器上的文件或目录",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "要删除的文件或目录路径",
					},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:        "dufs_list",
			Description: "列出 dufs 服务器上的目录内容",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "目录路径（默认为根目录）",
					},
					"query": map[string]interface{}{
						"type":        "string",
						"description": "搜索查询（可选）",
					},
					"format": map[string]interface{}{
						"type":        "string",
						"description": "输出格式：json, simple（可选）",
						"enum":        []string{"json", "simple"},
					},
				},
			},
		},
		{
			Name:        "dufs_create_dir",
			Description: "在 dufs 服务器上创建目录",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "要创建的目录路径",
					},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:        "dufs_move",
			Description: "移动或重命名 dufs 服务器上的文件或目录",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"source": map[string]interface{}{
						"type":        "string",
						"description": "源路径",
					},
					"destination": map[string]interface{}{
						"type":        "string",
						"description": "目标路径",
					},
				},
				"required": []string{"source", "destination"},
			},
		},
		{
			Name:        "dufs_get_hash",
			Description: "获取文件的 SHA256 哈希值",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "文件路径",
					},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:        "dufs_download_folder",
			Description: "下载整个文件夹为 zip 文件",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"remote_path": map[string]interface{}{
						"type":        "string",
						"description": "远程文件夹路径",
					},
					"local_path": map[string]interface{}{
						"type":        "string",
						"description": "本地保存路径（可选，默认为当前目录）",
					},
				},
				"required": []string{"remote_path"},
			},
		},
		{
			Name:        "dufs_health",
			Description: "检查 dufs 服务器健康状态",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
	}

	return &MCPServer{
		dufsClient: dufsClient,
		tools:      tools,
		config:     config,
	}
}

func (s *MCPServer) handleInitialize(params json.RawMessage) (interface{}, error) {
	return map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities": map[string]interface{}{
			"tools": map[string]interface{}{},
		},
		"serverInfo": map[string]interface{}{
			"name":    "dufs-mcp-server",
			"version": "1.0.0",
		},
	}, nil
}

func (s *MCPServer) handleToolsList(params json.RawMessage) (interface{}, error) {
	return map[string]interface{}{
		"tools": s.tools,
	}, nil
}

func (s *MCPServer) handleToolsCall(params json.RawMessage) (interface{}, error) {
	var callParams struct {
		Name      string                 `json:"name"`
		Arguments map[string]interface{} `json:"arguments"`
	}
	if err := json.Unmarshal(params, &callParams); err != nil {
		return nil, fmt.Errorf("invalid parameters: %v", err)
	}

	switch callParams.Name {
	case "dufs_upload":
		return s.handleUpload(callParams.Arguments)
	case "dufs_download":
		return s.handleDownload(callParams.Arguments)
	case "dufs_delete":
		return s.handleDelete(callParams.Arguments)
	case "dufs_list":
		return s.handleList(callParams.Arguments)
	case "dufs_create_dir":
		return s.handleCreateDir(callParams.Arguments)
	case "dufs_move":
		return s.handleMove(callParams.Arguments)
	case "dufs_get_hash":
		return s.handleGetHash(callParams.Arguments)
	case "dufs_download_folder":
		return s.handleDownloadFolder(callParams.Arguments)
	case "dufs_health":
		return s.handleHealth(callParams.Arguments)
	default:
		return nil, fmt.Errorf("unknown tool: %s", callParams.Name)
	}
}

func (s *MCPServer) handleUpload(args map[string]interface{}) (interface{}, error) {
	localPath, ok := args["local_path"].(string)
	if !ok {
		return nil, fmt.Errorf("local_path is required")
	}

	// 获取远程路径，如果未指定则自动生成
	remotePath, _ := args["remote_path"].(string)
	if remotePath == "" {
		// 自动创建远程路径：upload_dir/YYYYMMDD/文件名

		// 从本地路径提取文件名（支持 Unix 和 Windows 路径分隔符）
		fileName := localPath
		if lastSlash := strings.LastIndex(localPath, "/"); lastSlash >= 0 {
			fileName = localPath[lastSlash+1:]
		}
		if lastBackslash := strings.LastIndex(localPath, "\\"); lastBackslash >= 0 {
			fileName = localPath[lastBackslash+1:]
		}

		// 获取当前日期，格式：YYYYMMDD（例如：20251125）
		now := time.Now()
		dateDir := now.Format("20060102")

		// 构建远程路径：使用配置的 upload_dir 或默认的 "uploads"
		baseDir := s.config.UploadDir
		if baseDir == "" {
			baseDir = "uploads"
		}
		// 确保 baseDir 不以 / 开头，因为 makeRequest 会处理路径拼接
		baseDir = strings.TrimPrefix(baseDir, "/")

		// 生成完整路径：例如 uploads/20251125/file.txt
		remotePath = fmt.Sprintf("%s/%s/%s", baseDir, dateDir, fileName)
	}

	file, err := os.Open(localPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %v", err)
	}
	defer file.Close()

	// 确保远程路径的目录存在（递归创建）
	remoteDir := remotePath
	if lastSlash := strings.LastIndex(remotePath, "/"); lastSlash >= 0 {
		remoteDir = remotePath[:lastSlash]
	}
	if remoteDir != "" {
		// 递归创建目录路径
		parts := strings.Split(strings.TrimPrefix(remoteDir, "/"), "/")
		currentPath := ""
		for _, part := range parts {
			if part == "" {
				continue
			}
			if currentPath == "" {
				currentPath = part
			} else {
				currentPath = currentPath + "/" + part
			}
			// 尝试创建目录（如果不存在）
			resp, err := s.dufsClient.makeRequest("MKCOL", currentPath, nil, nil)
			if err == nil {
				resp.Body.Close()
				// 忽略 405 错误（Method Not Allowed），说明目录已存在
			}
		}
	}

	resp, err := s.dufsClient.makeRequest("PUT", remotePath, file, nil)
	if err != nil {
		return nil, fmt.Errorf("upload failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("upload failed with status %d: %s", resp.StatusCode, string(body))
	}

	return map[string]interface{}{
		"success":     true,
		"message":     fmt.Sprintf("File uploaded successfully to %s", remotePath),
		"remote_path": remotePath,
		"status":      resp.StatusCode,
	}, nil
}

func (s *MCPServer) handleDownload(args map[string]interface{}) (interface{}, error) {
	remotePath, ok := args["remote_path"].(string)
	if !ok {
		return nil, fmt.Errorf("remote_path is required")
	}

	localPath, _ := args["local_path"].(string)
	if localPath == "" {
		localPath = strings.TrimPrefix(strings.TrimPrefix(remotePath, "/"), "./")
		localPath = strings.ReplaceAll(localPath, "/", "_")
	}

	resp, err := s.dufsClient.makeRequest("GET", remotePath, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("download failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("download failed with status %d: %s", resp.StatusCode, string(body))
	}

	file, err := os.Create(localPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create local file: %v", err)
	}
	defer file.Close()

	written, err := io.Copy(file, resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to write file: %v", err)
	}

	return map[string]interface{}{
		"success":    true,
		"message":    fmt.Sprintf("File downloaded successfully to %s", localPath),
		"local_path": localPath,
		"size_bytes": written,
		"status":     resp.StatusCode,
	}, nil
}

func (s *MCPServer) handleDelete(args map[string]interface{}) (interface{}, error) {
	path, ok := args["path"].(string)
	if !ok {
		return nil, fmt.Errorf("path is required")
	}

	resp, err := s.dufsClient.makeRequest("DELETE", path, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("delete failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("delete failed with status %d: %s", resp.StatusCode, string(body))
	}

	return map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("Deleted %s successfully", path),
		"status":  resp.StatusCode,
	}, nil
}

func (s *MCPServer) handleList(args map[string]interface{}) (interface{}, error) {
	path := "/"
	if p, ok := args["path"].(string); ok && p != "" {
		path = p
	}

	query, _ := args["query"].(string)
	format, _ := args["format"].(string)

	url := path
	if query != "" {
		url += "?q=" + query
	} else if format != "" {
		url += "?" + format
	}

	resp, err := s.dufsClient.makeRequest("GET", url, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("list failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("list failed with status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %v", err)
	}

	var result interface{}
	if format == "json" {
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf("failed to parse JSON: %v", err)
		}
	} else {
		result = string(body)
	}

	return map[string]interface{}{
		"success": true,
		"data":    result,
		"status":  resp.StatusCode,
	}, nil
}

func (s *MCPServer) handleCreateDir(args map[string]interface{}) (interface{}, error) {
	path, ok := args["path"].(string)
	if !ok {
		return nil, fmt.Errorf("path is required")
	}

	resp, err := s.dufsClient.makeRequest("MKCOL", path, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("create directory failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("create directory failed with status %d: %s", resp.StatusCode, string(body))
	}

	return map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("Directory %s created successfully", path),
		"status":  resp.StatusCode,
	}, nil
}

func (s *MCPServer) handleMove(args map[string]interface{}) (interface{}, error) {
	source, ok := args["source"].(string)
	if !ok {
		return nil, fmt.Errorf("source is required")
	}
	destination, ok := args["destination"].(string)
	if !ok {
		return nil, fmt.Errorf("destination is required")
	}

	destURL := strings.TrimSuffix(s.dufsClient.BaseURL, "/") + "/" + strings.TrimPrefix(destination, "/")
	headers := map[string]string{
		"Destination": destURL,
	}

	resp, err := s.dufsClient.makeRequest("MOVE", source, nil, headers)
	if err != nil {
		return nil, fmt.Errorf("move failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("move failed with status %d: %s", resp.StatusCode, string(body))
	}

	return map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("Moved %s to %s successfully", source, destination),
		"status":  resp.StatusCode,
	}, nil
}

func (s *MCPServer) handleGetHash(args map[string]interface{}) (interface{}, error) {
	path, ok := args["path"].(string)
	if !ok {
		return nil, fmt.Errorf("path is required")
	}

	resp, err := s.dufsClient.makeRequest("GET", path+"?hash", nil, nil)
	if err != nil {
		return nil, fmt.Errorf("get hash failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get hash failed with status %d: %s", resp.StatusCode, string(body))
	}

	hash, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read hash: %v", err)
	}

	return map[string]interface{}{
		"success": true,
		"hash":    strings.TrimSpace(string(hash)),
		"path":    path,
	}, nil
}

func (s *MCPServer) handleDownloadFolder(args map[string]interface{}) (interface{}, error) {
	remotePath, ok := args["remote_path"].(string)
	if !ok {
		return nil, fmt.Errorf("remote_path is required")
	}

	localPath, _ := args["local_path"].(string)
	if localPath == "" {
		folderName := strings.TrimPrefix(strings.TrimPrefix(remotePath, "/"), "./")
		folderName = strings.ReplaceAll(folderName, "/", "_")
		localPath = folderName + ".zip"
	}

	resp, err := s.dufsClient.makeRequest("GET", remotePath+"?zip", nil, nil)
	if err != nil {
		return nil, fmt.Errorf("download folder failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("download folder failed with status %d: %s", resp.StatusCode, string(body))
	}

	file, err := os.Create(localPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create local file: %v", err)
	}
	defer file.Close()

	written, err := io.Copy(file, resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to write file: %v", err)
	}

	return map[string]interface{}{
		"success":    true,
		"message":    fmt.Sprintf("Folder downloaded successfully to %s", localPath),
		"local_path": localPath,
		"size_bytes": written,
		"status":     resp.StatusCode,
	}, nil
}

func (s *MCPServer) handleHealth(args map[string]interface{}) (interface{}, error) {
	resp, err := s.dufsClient.makeRequest("GET", "/__dufs__/health", nil, nil)
	if err != nil {
		return nil, fmt.Errorf("health check failed: %v", err)
	}
	defer resp.Body.Close()

	return map[string]interface{}{
		"success": resp.StatusCode == 200,
		"status":  resp.StatusCode,
		"healthy": resp.StatusCode == 200,
	}, nil
}

func (s *MCPServer) handleMessage(msg MCPMessage) MCPMessage {
	response := MCPMessage{
		JSONRPC: "2.0",
		ID:      msg.ID,
	}

	// 如果没有 method，可能是通知消息或无效消息
	if msg.Method == "" {
		response.Error = &MCPError{
			Code:    -32600,
			Message: "Invalid Request: method is required",
		}
		return response
	}

	var result interface{}
	var err error

	switch msg.Method {
	case "initialize":
		result, err = s.handleInitialize(msg.Params)
	case "tools/list":
		result, err = s.handleToolsList(msg.Params)
	case "tools/call":
		result, err = s.handleToolsCall(msg.Params)
	default:
		err = fmt.Errorf("unknown method: %s", msg.Method)
	}

	if err != nil {
		response.Error = &MCPError{
			Code:    -32000,
			Message: err.Error(),
		}
	} else {
		response.Result = result
	}

	return response
}

func loadConfig() (Config, error) {
	config := Config{
		DufsURL:       os.Getenv("DUFS_URL"),
		Username:      os.Getenv("DUFS_USERNAME"),
		Password:      os.Getenv("DUFS_PASSWORD"),
		UploadDir:     os.Getenv("DUFS_UPLOAD_DIR"),
		AllowInsecure: os.Getenv("DUFS_ALLOW_INSECURE") == "true",
	}

	if config.DufsURL == "" {
		return config, fmt.Errorf("DUFS_URL environment variable is required")
	}

	return config, nil
}

// runStdioMode 运行 stdio 模式（标准 MCP 协议）
func runStdioMode(server *MCPServer) {
	// 使用 stderr 输出日志，stdout 用于 JSON-RPC 通信
	log.SetOutput(os.Stderr)

	scanner := bufio.NewScanner(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var msg MCPMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			log.Printf("Failed to parse message: %v", err)
			// 发送解析错误响应
			errorResponse := MCPMessage{
				JSONRPC: "2.0",
				ID:      nil, // 如果无法解析，ID 可能也是无效的
				Error: &MCPError{
					Code:    -32700,
					Message: fmt.Sprintf("Parse error: %v", err),
				},
			}
			// 尝试从原始消息中提取 ID
			var rawMsg map[string]interface{}
			if json.Unmarshal([]byte(line), &rawMsg) == nil {
				if id, ok := rawMsg["id"]; ok {
					errorResponse.ID = id
				}
			}
			if encodeErr := encoder.Encode(errorResponse); encodeErr != nil {
				log.Printf("Failed to encode error response: %v", encodeErr)
			}
			continue
		}

		// 确保消息有 ID（对于通知消息，ID 可能为 nil）
		response := server.handleMessage(msg)

		// 只有请求消息（有 ID）才需要响应
		if msg.ID != nil {
			if err := encoder.Encode(response); err != nil {
				log.Printf("Failed to encode response: %v", err)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		log.Fatalf("Scanner error: %v", err)
	}
}

// runHTTPMode 运行 HTTP/SSE 模式
func runHTTPMode(server *MCPServer, port string) {
	// SSE 端点：用于接收服务器推送的消息
	http.HandleFunc("/sse", func(w http.ResponseWriter, r *http.Request) {
		// 设置 SSE headers
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "SSE not supported", http.StatusInternalServerError)
			return
		}

		// 发送初始连接消息
		fmt.Fprintf(w, "data: %s\n\n", `{"type":"connection","status":"connected"}`)
		flusher.Flush()

		// 保持连接打开，等待客户端关闭
		<-r.Context().Done()
	})

	// 接收客户端消息的端点
	http.HandleFunc("/message", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		if r.Method != "POST" {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var msg MCPMessage
		if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
			http.Error(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
			return
		}

		response := server.handleMessage(msg)
		json.NewEncoder(w).Encode(response)
	})

	log.Printf("MCP Server (HTTP mode) starting on port %s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func main() {
	config, err := loadConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	if config.DufsURL == "" {
		log.Fatal("DUFS_URL is required")
	}

	server := NewMCPServer(config)

	// 根据环境变量选择运行模式
	mode := os.Getenv("MCP_MODE")
	if mode == "" {
		// 默认使用 stdio 模式（标准 MCP 协议）
		mode = "stdio"
	}

	switch mode {
	case "stdio":
		// stdio 模式：标准 MCP 协议，通过 stdin/stdout 通信
		log.SetOutput(os.Stderr)
		log.Printf("MCP Server (stdio mode) starting")
		log.Printf("Dufs URL: %s", config.DufsURL)
		runStdioMode(server)
	case "http", "sse":
		// HTTP/SSE 模式：通过 HTTP 端点通信
		port := os.Getenv("PORT")
		if port == "" {
			port = "7887"
		}
		log.Printf("Dufs URL: %s", config.DufsURL)
		runHTTPMode(server, port)
	default:
		log.Fatalf("Unknown MCP_MODE: %s. Supported modes: stdio, http, sse", mode)
	}
}
