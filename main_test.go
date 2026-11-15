package main

import (
    "bytes"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "net/http/httptest"
    "os"
    "path/filepath"
    "strings"
    "sync/atomic"
    "testing"
)

func toolResult(t *testing.T, s *MCPServer, name string, args map[string]interface{}) map[string]interface{} {
    t.Helper()
    params, err := json.Marshal(map[string]interface{}{"name": name, "arguments": args})
    if err != nil { t.Fatal(err) }
    result, err := s.handleToolsCall(params)
    if err != nil { t.Fatalf("tools/call %s transport error: %v", name, err) }
    m, ok := result.(map[string]interface{})
    if !ok { t.Fatalf("invalid tool result: %#v", result) }
    return m
}

func assertIsError(t *testing.T, m map[string]interface{}, want bool) {
    t.Helper()
    got, ok := m["isError"].(bool)
    if !ok || got != want { t.Fatalf("want isError=%v, got %#v", want, m) }
    if _, ok := m["content"]; !ok { t.Fatalf("missing MCP content: %#v", m) }
}

func configureReadOnly(t *testing.T) string {
    t.Helper()
    root := t.TempDir()
    t.Setenv("DUFS_LOCAL_ROOT", root)
    t.Setenv("DUFS_REMOTE_ROOT", "uploads")
    t.Setenv("DUFS_MCP_ENABLE_UPLOAD", "")
    t.Setenv("DUFS_MCP_ENABLE_DELETE", "")
    t.Setenv("DUFS_MCP_ENABLE_MOVE", "")
    return root
}

func TestReadonlyDefaultAndMissingArguments(t *testing.T) {
    configureReadOnly(t)
    var calls atomic.Int32
    upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        calls.Add(1)
        w.WriteHeader(http.StatusOK)
    }))
    defer upstream.Close()
    s := NewMCPServer(Config{DufsURL: upstream.URL, UploadDir: "/uploads"})
    denied := []struct {
        name string
        args map[string]interface{}
    }{
        {"dufs_upload", map[string]interface{}{"local_path": "input.txt"}},
        {"dufs_upload_batch", map[string]interface{}{"files": []interface{}{map[string]interface{}{"local_path": "input.txt"}}}},
        {"dufs_delete", map[string]interface{}{"path": "/uploads/doc"}},
        {"dufs_move", map[string]interface{}{"source": "/uploads/a", "destination": "/uploads/b"}},
        {"dufs_create_dir", map[string]interface{}{"path": "/uploads/sub"}},
    }
    for _, tc := range denied {
        t.Run(tc.name, func(t *testing.T) {
            assertIsError(t, toolResult(t, s, tc.name, tc.args), true)
        })
    }
    assertIsError(t, toolResult(t, s, "dufs_list", nil), false) // no nil-map panic
    if got := calls.Load(); got != 1 { t.Fatalf("denied operations accessed Dufs: %d calls", got) }
}

func TestRemoteRootAndQueryValidation(t *testing.T) {
    configureReadOnly(t)
    s := NewMCPServer(Config{UploadDir: "/uploads"})
    allowed := []string{"/uploads", "uploads/reports/a.txt", "uploads/中文 文件.md"}
    for _, path := range allowed {
        if err := s.validateRemote(path); err != nil { t.Errorf("rejected %q: %v", path, err) }
    }
    denied := []string{"", "/", "/private/file", "uploads/../secret", "uploads//doc", "uploads/%2e%2e/x", "uploads/a?hash", "uploads/a#part", "uploads/a\\b", "uploads/./file", "uploads/a\x00b"}
    for _, path := range denied {
        if err := s.validateRemote(path); err == nil { t.Errorf("accepted unsafe path %q", path) }
    }
    assertIsError(t, toolResult(t, s, "dufs_list", map[string]interface{}{"path": "/uploads", "format": "xml"}), true)
    assertIsError(t, toolResult(t, s, "dufs_list", map[string]interface{}{"path": "/uploads", "query": strings.Repeat("x", 257)}), true)
}

func TestLocalRootBoundariesAndNoOverwrite(t *testing.T) {
    local := configureReadOnly(t)
    outside := t.TempDir()
    if err := os.WriteFile(filepath.Join(outside, "private.txt"), []byte("private"), 0600); err != nil { t.Fatal(err) }
    if _, err := openScopedInput(filepath.Join(outside, "private.txt")); err == nil { t.Fatal("read escaped local root") }
    if _, err := copyScopedOutput("../escape.txt", strings.NewReader("escape")); err == nil { t.Fatal("write escaped local root") }
    if err := os.WriteFile(filepath.Join(local, "existing.txt"), []byte("original"), 0600); err != nil { t.Fatal(err) }
    if _, err := copyScopedOutput("existing.txt", strings.NewReader("overwrite")); err == nil { t.Fatal("overwrote existing file") }
    got, err := os.ReadFile(filepath.Join(local, "existing.txt"))
    if err != nil || string(got) != "original" { t.Fatalf("existing file changed: %q %v", got, err) }
    if err := os.Symlink(outside, filepath.Join(local, "outside-link")); err == nil {
        if _, err := copyScopedOutput("outside-link/new.txt", strings.NewReader("escape")); err == nil {
            t.Fatal("symlink escaped local root on write")
        }
        if _, err := openScopedInput("outside-link/private.txt"); err == nil { t.Fatal("symlink escaped local root on read") }
    }
}

func TestMCPDownloadAndHTTPFailure(t *testing.T) {
    local := configureReadOnly(t)
    payload := []byte("real bytes received from Dufs")
    upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        switch r.URL.Path {
        case "/uploads/asset.bin":
            if r.URL.RawQuery != "" { t.Errorf("unexpected query %q", r.URL.RawQuery) }
            w.Write(payload)
        case "/uploads/missing.bin":
            http.NotFound(w, r)
        case "/uploads/broken.bin":
            w.Header().Set("Content-Length", "100")
            _, _ = w.Write([]byte("short"))
        default:
            http.Error(w, "unknown", http.StatusNotFound)
        }
    }))
    defer upstream.Close()
    s := NewMCPServer(Config{DufsURL: upstream.URL})
    assertIsError(t, toolResult(t, s, "dufs_download", map[string]interface{}{"remote_path": "/uploads/asset.bin"}), false)
    got, err := os.ReadFile(filepath.Join(local, "asset.bin"))
    if err != nil || !bytes.Equal(got, payload) { t.Fatalf("download content mismatch: %q %v", got, err) }
    assertIsError(t, toolResult(t, s, "dufs_download", map[string]interface{}{"remote_path": "/uploads/asset.bin"}), true)
    assertIsError(t, toolResult(t, s, "dufs_download", map[string]interface{}{"remote_path": "/uploads/missing.bin"}), true)
    assertIsError(t, toolResult(t, s, "dufs_download", map[string]interface{}{"remote_path": "/uploads/broken.bin"}), true)
    if _, err := os.Stat(filepath.Join(local, "broken.bin")); !os.IsNotExist(err) { t.Fatalf("partial download retained: %v", err) }
}

func TestMCPUploadRequiresOptInAndScopedFile(t *testing.T) {
    local := configureReadOnly(t)
    t.Setenv("DUFS_MCP_ENABLE_UPLOAD", "true")
    payload := []byte("direct MCP upload only when explicitly authorized")
    if err := os.WriteFile(filepath.Join(local, "input.txt"), payload, 0600); err != nil { t.Fatal(err) }
    uploaded := make(chan []byte, 1)
    upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        switch r.Method {
        case "MKCOL":
            w.WriteHeader(http.StatusCreated)
        case "PUT":
            if r.URL.Path != "/uploads/run/input.txt" { t.Errorf("unexpected PUT path: %s", r.URL.Path) }
            b, err := io.ReadAll(r.Body)
            if err != nil { t.Errorf("read upload body: %v", err) }
            uploaded <- b
            w.WriteHeader(http.StatusCreated)
        default:
            http.Error(w, "invalid method", http.StatusMethodNotAllowed)
        }
    }))
    defer upstream.Close()
    s := NewMCPServer(Config{DufsURL: upstream.URL})
    assertIsError(t, toolResult(t, s, "dufs_upload", map[string]interface{}{"local_path": "input.txt", "remote_path": "uploads/run/input.txt"}), false)
    got := <-uploaded
    if !bytes.Equal(got, payload) { t.Fatalf("Dufs received %q", got) }
    assertIsError(t, toolResult(t, s, "dufs_upload", map[string]interface{}{"local_path": "input.txt", "remote_path": "other/input.txt"}), true)
    assertIsError(t, toolResult(t, s, "dufs_upload", map[string]interface{}{"local_path": filepath.Join(t.TempDir(), "private"), "remote_path": "uploads/run/private"}), true)
}

func TestHTTPAuthorizationAndNoFakeSSE(t *testing.T) {
    configureReadOnly(t)
    token := strings.Repeat("a", 48)
    handler := newMCPHTTPHandler(NewMCPServer(Config{DufsURL: "http://127.0.0.1:5000"}), token)
    data := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`
    run := func(method, path, auth, origin string) *httptest.ResponseRecorder {
        req := httptest.NewRequest(method, path, strings.NewReader(data))
        if auth != "" { req.Header.Set("Authorization", auth) }
        if origin != "" { req.Header.Set("Origin", origin) }
        rr := httptest.NewRecorder()
        handler.ServeHTTP(rr, req)
        return rr
    }
    if got := run("POST", "/message", "", "").Code; got != 401 { t.Fatalf("no token: %d", got) }
    if got := run("POST", "/message", "Bearer wrong", "").Code; got != 401 { t.Fatalf("wrong token: %d", got) }
    bearer := "Bearer " + token
    if got := run("POST", "/message", bearer, "https://attacker.test").Code; got != 403 { t.Fatalf("Origin accepted: %d", got) }
    if got := run("GET", "/message", bearer, "").Code; got != 405 { t.Fatalf("wrong method: %d", got) }
    if got := run("GET", "/sse", bearer, "").Code; got != 501 { t.Fatalf("nonfunctional SSE misadvertised: %d", got) }
    rr := run("POST", "/message", bearer, "")
    if rr.Code != 200 { t.Fatalf("authorized JSON-RPC HTTP %d: %s", rr.Code, rr.Body.String()) }
    var response map[string]interface{}
    if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil { t.Fatal(err) }
    if _, ok := response["result"]; !ok { t.Fatalf("no initialize result: %#v", response) }
}

func TestDisallowOutboundRedirect(t *testing.T) {
    configureReadOnly(t)
    var calls atomic.Int32
    upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        calls.Add(1)
        http.Redirect(w, r, "/private", http.StatusFound)
    }))
    defer upstream.Close()
    s := NewMCPServer(Config{DufsURL: upstream.URL})
    assertIsError(t, toolResult(t, s, "dufs_list", map[string]interface{}{"path": "/uploads"}), true)
    if calls.Load() != 1 { t.Fatalf("followed redirect to unexpected resource: %d calls", calls.Load()) }
}

func TestLoadConfigRejectsUnapprovedHTTP(t *testing.T) {
    t.Setenv("DUFS_ALLOW_INSECURE", "")
    for _, target := range []string{"file:///etc/passwd", "http://example.test:5000", "http://admin:pw@example.test"} {
        t.Setenv("DUFS_URL", target)
        if _, err := loadConfig(); err == nil { t.Errorf("unsafe URL accepted: %q", target) }
    }
    t.Setenv("DUFS_URL", "https://example.test")
    if _, err := loadConfig(); err != nil { t.Fatalf("HTTPS rejected: %v", err) }
    t.Setenv("DUFS_URL", "http://127.0.0.1:5000")
    if _, err := loadConfig(); err != nil { t.Fatalf("loopback rejected: %v", err) }
    t.Setenv("DUFS_URL", "http://192.0.2.10:5000")
    t.Setenv("DUFS_ALLOW_INSECURE", "true")
    if _, err := loadConfig(); err != nil { t.Fatalf("explicit insecure opt-in rejected: %v", err) }
}

func TestReadLimitAndStatus(t *testing.T) {
    configureReadOnly(t)
    upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.URL.Path == "/uploads/list" {
            _, _ = io.CopyN(w, strings.NewReader(strings.Repeat("x", (2<<20)+10)), (2<<20)+10)
            return
        }
        if r.URL.Path == "/uploads/nope" {
            w.WriteHeader(http.StatusFound)
            fmt.Fprint(w, "redirect")
            return
        }
        w.WriteHeader(http.StatusNotFound)
    }))
    defer upstream.Close()
    s := NewMCPServer(Config{DufsURL: upstream.URL})
    assertIsError(t, toolResult(t, s, "dufs_list", map[string]interface{}{"path": "/uploads/list"}), true)
    assertIsError(t, toolResult(t, s, "dufs_list", map[string]interface{}{"path": "/uploads/nope"}), true)
}

func TestUploadTaskCapacity(t *testing.T) {
    local := configureReadOnly(t)
    t.Setenv("DUFS_MCP_ENABLE_UPLOAD", "true")
    if err := os.WriteFile(filepath.Join(local, "f"), []byte("x"), 0600); err != nil { t.Fatal(err) }
    s := NewMCPServer(Config{DufsURL: "http://127.0.0.1:5000"})
    for i := 0; i < 128; i++ {
        s.jobs[fmt.Sprint(i)] = &UploadJob{Status: "running"}
    }
    assertIsError(t, toolResult(t, s, "dufs_upload", map[string]interface{}{"local_path": "f", "async": true}), true)
    if len(s.jobs) != 128 { t.Fatal("upload capacity exceeded") }
}

func TestProjectScopedUploadsNeverCreateParentDirectory(t *testing.T) {
    local := configureReadOnly(t)
    t.Setenv("DUFS_REMOTE_ROOT", "projects/apios")
    t.Setenv("DUFS_MCP_ENABLE_UPLOAD", "true")
    if err := os.WriteFile(filepath.Join(local, "artifact.txt"), []byte("artifact"), 0600); err != nil { t.Fatal(err) }
    calls := make(chan string, 8)
    upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        calls <- r.Method + " " + r.URL.Path
        if r.Method == "MKCOL" {
            if r.URL.Path == "/projects" || r.URL.Path == "/projects/apios" {
                http.Error(w, "must not create project parents", http.StatusForbidden)
                return
            }
            w.WriteHeader(http.StatusCreated)
            return
        }
        if r.Method == "PUT" { w.WriteHeader(http.StatusCreated); return }
        http.NotFound(w, r)
    }))
    defer upstream.Close()
    s := NewMCPServer(Config{DufsURL: upstream.URL})
    assertIsError(t, toolResult(t, s, "dufs_upload", map[string]interface{}{
        "local_path": "artifact.txt", "remote_path": "projects/apios/20261115/artifact.txt",
    }), false)
    if n := len(calls); n != 2 { t.Fatalf("expected only scoped mkdir and PUT, got %d calls", n) }
    a, b := <-calls, <-calls
    if a != "MKCOL /projects/apios/20261115" || b != "PUT /projects/apios/20261115/artifact.txt" {
        t.Fatalf("unexpected upstream operations: %q, %q", a, b)
    }
}

func TestToolsListOnlyAdvertisesAuthorizedOperations(t *testing.T) {
    t.Setenv("DUFS_LOCAL_ROOT", "")
    t.Setenv("DUFS_MCP_ENABLE_UPLOAD", "")
    t.Setenv("DUFS_MCP_ENABLE_DELETE", "")
    t.Setenv("DUFS_MCP_ENABLE_MOVE", "")
    s := NewMCPServer(Config{})
    get := func() map[string]bool {
        raw, err := s.handleToolsList(nil)
        if err != nil { t.Fatal(err) }
        result, ok := raw.(map[string]interface{})
        if !ok { t.Fatal("invalid list result") }
        names := map[string]bool{}
        for _, tool := range result["tools"].([]MCPTool) { names[tool.Name] = true }
        return names
    }
    names := get()
    if !names["dufs_list"] || names["dufs_download"] || names["dufs_delete"] || names["dufs_upload"] {
        t.Fatalf("unsafe capabilities advertised by default: %#v", names)
    }
    root := t.TempDir()
    t.Setenv("DUFS_LOCAL_ROOT", root)
    t.Setenv("DUFS_MCP_ENABLE_UPLOAD", "true")
    names = get()
    if !names["dufs_download"] || !names["dufs_upload"] || names["dufs_delete"] {
        t.Fatalf("capabilities inconsistent with configured permissions: %#v", names)
    }
}

func TestUnhealthyServiceReportsToolError(t *testing.T) {
    configureReadOnly(t)
    upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusServiceUnavailable)
    }))
    defer upstream.Close()
    s := NewMCPServer(Config{DufsURL: upstream.URL})
    assertIsError(t, toolResult(t, s, "dufs_health", map[string]interface{}{}), true)
}
