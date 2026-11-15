package main

import (
    "errors"
    "fmt"
    "io"
    "os"
    "path/filepath"
    "strings"
    "unicode/utf8"
)

const maxMCPFileBytes int64 = 1 << 30 // 1 GiB per transfer, CI uses Dufs directly.

func enabled(key string) bool { return os.Getenv(key) == "true" }

func validRemoteParts(value string) bool {
    if value == "" || !utf8.ValidString(value) || strings.ContainsAny(value, "\\?#%") {
        return false
    }
    value = strings.TrimPrefix(value, "/")
    if value == "" || strings.HasPrefix(value, "/") {
        return false
    }
    for _, part := range strings.Split(value, "/") {
        if part == "" || part == "." || part == ".." {
            return false
        }
        for _, r := range part {
            if r < 32 || r == 127 {
                return false
            }
        }
    }
    return true
}

func (s *MCPServer) remoteRoot() string {
    root := os.Getenv("DUFS_REMOTE_ROOT")
    if root == "" {
        root = s.config.UploadDir
    }
    if root == "" {
        root = "uploads"
    }
    return strings.TrimPrefix(root, "/")
}

func (s *MCPServer) validateRemote(p string) error {
    if !validRemoteParts(p) {
        return fmt.Errorf("invalid remote path")
    }
    root := s.remoteRoot()
    if !validRemoteParts(root) {
        return fmt.Errorf("invalid DUFS_REMOTE_ROOT")
    }
    p = strings.TrimPrefix(p, "/")
    if p != root && !strings.HasPrefix(p, root+"/") {
        return fmt.Errorf("remote path outside DUFS_REMOTE_ROOT")
    }
    return nil
}

// scopedLocalRoot uses os.Root (Go 1.24+) to prevent symlink races and
// traversal beyond the operator-configured local file exchange directory.
func scopedLocalRoot(localPath string) (*os.Root, string, error) {
    base := os.Getenv("DUFS_LOCAL_ROOT")
    if !filepath.IsAbs(base) || filepath.Clean(base) == string(filepath.Separator) {
        return nil, "", errors.New("DUFS_LOCAL_ROOT must be an absolute dedicated directory")
    }
    if localPath == "" {
        return nil, "", errors.New("local_path is required")
    }
    root, err := os.OpenRoot(base)
    if err != nil {
        return nil, "", fmt.Errorf("DUFS_LOCAL_ROOT unavailable: %w", err)
    }
    rel := filepath.Clean(localPath)
    if filepath.IsAbs(rel) {
        rel, err = filepath.Rel(filepath.Clean(base), rel)
        if err != nil {
            root.Close()
            return nil, "", err
        }
    }
    if rel == "." || rel == ".." || filepath.IsAbs(rel) ||
        strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
        root.Close()
        return nil, "", errors.New("local path outside DUFS_LOCAL_ROOT")
    }
    return root, rel, nil
}

func openScopedInput(localPath string) (*os.File, error) {
    root, rel, err := scopedLocalRoot(localPath)
    if err != nil {
        return nil, err
    }
    defer root.Close()
    f, err := root.Open(rel)
    if err != nil {
        return nil, err
    }
    info, err := f.Stat()
    if err != nil || !info.Mode().IsRegular() || info.Size() > maxMCPFileBytes {
        f.Close()
        return nil, errors.New("source must be a regular file no larger than 1 GiB")
    }
    return f, nil
}

func copyScopedOutput(localPath string, source io.Reader) (int64, error) {
    root, rel, err := scopedLocalRoot(localPath)
    if err != nil {
        return 0, err
    }
    defer root.Close()
    // Atomic creation: existing files, including symlinks, are never overwritten.
    f, err := root.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
    if err != nil {
        return 0, err
    }
    // If transfer fails or is interrupted, remove partial output.
    success := false
    defer func() {
        f.Close()
        if !success {
            _ = root.Remove(rel)
        }
    }()
    written, err := io.Copy(f, io.LimitReader(source, maxMCPFileBytes+1))
    if err != nil {
        return 0, err
    }
    if written > maxMCPFileBytes {
        return 0, errors.New("download exceeded 1 GiB limit")
    }
    if err = f.Sync(); err != nil {
        return 0, err
    }
    if err = f.Close(); err != nil {
        return 0, err
    }
    success = true
    return written, nil
}

func (s *MCPServer) authorizeTool(name string, args map[string]interface{}) error {
    checkRemote := func(key string) error {
        p, ok := args[key].(string)
        if !ok || p == "" {
            return fmt.Errorf("%s is required", key)
        }
        return s.validateRemote(p)
    }
    checkLocal := func(key string) error {
        p, ok := args[key].(string)
        if !ok || p == "" {
            return fmt.Errorf("%s is required", key)
        }
        root, _, err := scopedLocalRoot(p)
        if root != nil {
            root.Close()
        }
        return err
    }
    switch name {
    case "dufs_health", "dufs_upload_status":
        return nil
    case "dufs_list":
        p, _ := args["path"].(string)
        if p == "" || p == "/" {
            p = "/" + s.remoteRoot()
            args["path"] = p
        }
        format, _ := args["format"].(string)
        if format != "" && format != "json" && format != "simple" {
            return fmt.Errorf("invalid list format")
        }
        if query, ok := args["query"].(string); ok && len(query) > 256 {
            return fmt.Errorf("search query too long")
        }
        return s.validateRemote(p)
    case "dufs_get_hash":
        return checkRemote("path")
    case "dufs_download", "dufs_download_folder":
        if err := checkRemote("remote_path"); err != nil {
            return err
        }
        p, _ := args["local_path"].(string)
        if p == "" {
            p = filepath.Base(strings.TrimSuffix(args["remote_path"].(string), "/"))
            if name == "dufs_download_folder" {
                p += ".zip"
            }
            args["local_path"] = p // relative to DUFS_LOCAL_ROOT, never working directory
        }
        return checkLocal("local_path")
    case "dufs_upload":
        if !enabled("DUFS_MCP_ENABLE_UPLOAD") {
            return errors.New("upload capability disabled")
        }
        if err := checkLocal("local_path"); err != nil {
            return err
        }
        if p, _ := args["remote_path"].(string); p != "" {
            return s.validateRemote(p)
        }
        return s.validateRemote(s.resolveRemotePath(args["local_path"].(string), ""))
    case "dufs_upload_batch":
        if !enabled("DUFS_MCP_ENABLE_UPLOAD") {
            return errors.New("upload capability disabled")
        }
        files, ok := args["files"].([]interface{})
        if !ok || len(files) == 0 || len(files) > 32 {
            return errors.New("upload batch must contain 1 to 32 files")
        }
        for _, item := range files {
            file, ok := item.(map[string]interface{})
            if !ok {
                return errors.New("invalid file entry")
            }
            if err := s.authorizeTool("dufs_upload", file); err != nil {
                return err
            }
        }
        return nil
    case "dufs_create_dir":
        if !enabled("DUFS_MCP_ENABLE_UPLOAD") {
            return errors.New("directory creation capability disabled")
        }
        return checkRemote("path")
    case "dufs_delete":
        if !enabled("DUFS_MCP_ENABLE_DELETE") {
            return errors.New("delete capability disabled")
        }
        return checkRemote("path")
    case "dufs_move":
        if !enabled("DUFS_MCP_ENABLE_MOVE") {
            return errors.New("move capability disabled")
        }
        if err := checkRemote("source"); err != nil {
            return err
        }
        return checkRemote("destination")
    default:
        return fmt.Errorf("unknown tool: %s", name)
    }
}
