package main

import (
    "bytes"
    "crypto/sha256"
    "encoding/hex"
    "fmt"
    "io"
    "net/http"
    "os"
    "path/filepath"
    "strings"
    "testing"
    "time"
)

// TestRealDufsDirectCIAndMCP runs against a real Dufs process, not a
// httptest replacement. Environment is supplied only by the self-hosted CI
// integration job; ordinary go test runs skip this isolation test.
func TestRealDufsDirectCIAndMCP(t *testing.T) {
    baseURL := os.Getenv("DUFS_INTEGRATION_URL")
    if baseURL == "" {
        t.Skip("requires a locally started, version-pinned real Dufs")
    }
    ciUser := os.Getenv("DUFS_INTEGRATION_CI_USER")
    ciPass := os.Getenv("DUFS_INTEGRATION_CI_PASSWORD")
    readerUser := os.Getenv("DUFS_INTEGRATION_MCP_USER")
    readerPass := os.Getenv("DUFS_INTEGRATION_MCP_PASSWORD")
    if ciUser == "" || ciPass == "" || readerUser == "" || readerPass == "" {
        t.Fatal("missing integration test credentials")
    }
    baseURL = strings.TrimRight(baseURL, "/")
    fileBytes := []byte("real Dufs isolated CI upload and MCP read\n")
    expected := sha256.Sum256(fileBytes)
    expectedHex := hex.EncodeToString(expected[:])
    path := fmt.Sprintf("projects/apios/verification-%d/ci-proof.txt", time.Now().UnixNano())
    directory := path[:strings.LastIndex(path, "/")]

    request := func(method, location, user, password string, body io.Reader) *http.Response {
        t.Helper()
        req, err := http.NewRequest(method, baseURL+"/"+location, body)
        if err != nil { t.Fatal(err) }
        if user != "" { req.SetBasicAuth(user, password) }
        response, err := http.DefaultClient.Do(req)
        if err != nil { t.Fatalf("%s %s: %v", method, location, err) }
        return response
    }
    checkSuccess := func(response *http.Response, label string) {
        t.Helper()
        defer response.Body.Close()
        if response.StatusCode < 200 || response.StatusCode >= 300 {
            part, _ := io.ReadAll(io.LimitReader(response.Body, 512))
            t.Fatalf("%s HTTP %d: %s", label, response.StatusCode, part)
        }
    }

    // No MCP process or tool call is involved: direct CI HTTP PUT.
    checkSuccess(request("MKCOL", directory, ciUser, ciPass, nil), "CI MKCOL")
    checkSuccess(request("PUT", path, ciUser, ciPass, bytes.NewReader(fileBytes)), "CI PUT")

    // Reader account is project-scoped and does not have write permission.
    denied := request("PUT", path, readerUser, readerPass, bytes.NewReader([]byte("bad")))
    denied.Body.Close()
    if denied.StatusCode < 400 { t.Fatalf("MCP read-only account could write: HTTP %d", denied.StatusCode) }

    // Anonymous readers must never obtain the artifact.
    anonymous := request("GET", path, "", "", nil)
    anonymous.Body.Close()
    if anonymous.StatusCode < 400 { t.Fatalf("anonymous Dufs GET disclosed artifact: HTTP %d", anonymous.StatusCode) }

    hResp := request("GET", path+"?hash", ciUser, ciPass, nil)
    if hResp.StatusCode != 200 {
        hResp.Body.Close()
        t.Fatalf("Dufs ?hash HTTP %d (requires --allow-hash)", hResp.StatusCode)
    }
    hBytes, err := io.ReadAll(io.LimitReader(hResp.Body, 128))
    hResp.Body.Close()
    if err != nil || strings.TrimSpace(string(hBytes)) != expectedHex {
        t.Fatalf("Dufs hash does not match CI bytes: %q, err=%v expected=%s", hBytes, err, expectedHex)
    }

    // MCP is an independent read-only client of the same Dufs instance.
    local := t.TempDir()
    t.Setenv("DUFS_LOCAL_ROOT", local)
    t.Setenv("DUFS_REMOTE_ROOT", "projects/apios")
    t.Setenv("DUFS_MCP_ENABLE_UPLOAD", "")
    t.Setenv("DUFS_MCP_ENABLE_DELETE", "")
    t.Setenv("DUFS_MCP_ENABLE_MOVE", "")
    client := NewMCPServer(Config{
        DufsURL: baseURL, Username: readerUser, Password: readerPass,
    })

    assertIsError(t, toolResult(t, client, "dufs_download", map[string]interface{}{
        "remote_path": path, "local_path": "ci-proof.txt",
    }), false)
    downloaded, err := os.ReadFile(filepath.Join(local, "ci-proof.txt"))
    if err != nil || !bytes.Equal(downloaded, fileBytes) {
        t.Fatalf("real Dufs MCP download bytes mismatch: %q (%v)", downloaded, err)
    }
    assertIsError(t, toolResult(t, client, "dufs_get_hash", map[string]interface{}{
        "path": path,
    }), false)
    assertIsError(t, toolResult(t, client, "dufs_delete", map[string]interface{}{
        "path": path,
    }), true)

    // Optional remote root, not server filesystem root, is the default list.
    assertIsError(t, toolResult(t, client, "dufs_list", map[string]interface{}{
        "path": "projects/apios", "format": "json",
    }), false)

    // A failed write must not corrupt the CI artifact.
    final := request("GET", path, ciUser, ciPass, nil)
    if final.StatusCode != 200 { final.Body.Close(); t.Fatalf("final download HTTP %d", final.StatusCode) }
    bytesAfter, err := io.ReadAll(io.LimitReader(final.Body, 1<<20))
    final.Body.Close()
    if err != nil || !bytes.Equal(bytesAfter, fileBytes) {
        t.Fatalf("CI artifact changed after unauthorized operation: %q (%v)", bytesAfter, err)
    }
}
