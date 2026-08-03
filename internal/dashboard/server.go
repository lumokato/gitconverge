// 本文件提供仅监听本机随机端口的只读状态页，并允许安全触发一次同步。
package dashboard

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lumokato/gitconverge/internal/config"
	"github.com/lumokato/gitconverge/internal/execx"
	"github.com/lumokato/gitconverge/internal/install"
	"github.com/lumokato/gitconverge/internal/lock"
)

type Server struct {
	ConfigPath string
	WorkerPath string
	TrayPath   string
	Token      string
	URL        string
	server     *http.Server
	runtime    Runtime
	managed    bool
	runMu      sync.Mutex
}

func Start(configPath, workerPath string) (*Server, error) {
	token, err := randomToken()
	if err != nil {
		return nil, err
	}
	trayPath, err := os.Executable()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	managed := install.ManagedInstallation(trayPath, workerPath)
	runtime := Runtime{}
	if managed {
		runtime.WorkerInstalled = install.WorkerInstalled(ctx, execx.Runner{})
		runtime.TrayAutostart = install.TrayAutostartEnabled()
	}
	server := &Server{
		ConfigPath: configPath,
		WorkerPath: workerPath,
		TrayPath:   trayPath,
		managed:    managed,
		Token:      token,
		runtime:    runtime,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", server.auth(server.index))
	mux.HandleFunc("/api/status", server.auth(server.statusAPI))
	mux.HandleFunc("/api/run", server.auth(server.runAPI))
	mux.HandleFunc("/api/config", server.auth(server.configAPI))
	mux.HandleFunc("/api/logs", server.auth(server.logsAPI))
	mux.HandleFunc("/api/folder", server.auth(server.folderAPI))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	server.URL = "http://" + listener.Addr().String() + "/?token=" + token
	server.server = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = server.server.Serve(listener) }()
	return server, nil
}

func (server *Server) Snapshot() (Snapshot, error) {
	return Load(server.ConfigPath, server.runtime)
}

func (server *Server) RunNow() error {
	server.runMu.Lock()
	defer server.runMu.Unlock()
	if _, err := LoadConfigurationDocument(server.ConfigPath); err != nil {
		return err
	}
	if snapshot, err := server.Snapshot(); err != nil || !snapshot.Configured {
		return fmt.Errorf("请先完成配置")
	}
	active, err := lock.Active("GitConverge")
	if err != nil {
		return err
	}
	if active {
		return fmt.Errorf("同步已在运行")
	}
	command := exec.Command(server.WorkerPath, "run", "--config", server.ConfigPath)
	configureCommand(command)
	if err := command.Start(); err != nil {
		return err
	}
	return command.Process.Release()
}

func (server *Server) SaveConfiguration(document ConfigurationDocument) error {
	configuration, err := SaveConfigurationDocument(server.ConfigPath, document)
	if err != nil {
		return err
	}
	if server.managed {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := install.Update(ctx, server.TrayPath, server.WorkerPath, configuration.SourcePath, configuration.Interval.Duration, configuration.RunTimeout.Duration, execx.Runner{}); err != nil {
			return fmt.Errorf("update scheduled task: %w", err)
		}
		server.runtime.WorkerInstalled = true
		server.runtime.TrayAutostart = true
	}
	return nil
}

func (server *Server) EnsureInstallation() error {
	if !server.managed || (server.runtime.WorkerInstalled && server.runtime.TrayAutostart) {
		return nil
	}
	configuration, err := config.Load(server.ConfigPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := install.Update(ctx, server.TrayPath, server.WorkerPath, configuration.SourcePath, configuration.Interval.Duration, configuration.RunTimeout.Duration, execx.Runner{}); err != nil {
		return err
	}
	server.runtime.WorkerInstalled = true
	server.runtime.TrayAutostart = true
	return nil
}

func (server *Server) Open() error { return openURL(server.URL) }

func (server *Server) Shutdown(ctx context.Context) error {
	if server == nil || server.server == nil {
		return nil
	}
	return server.server.Shutdown(ctx)
}

func (server *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Query().Get("token") != server.Token && request.Header.Get("X-GitConverge-Token") != server.Token {
			http.Error(writer, "forbidden", http.StatusForbidden)
			return
		}
		next(writer, request)
	}
}

func (server *Server) index(writer http.ResponseWriter, request *http.Request) {
	if request.URL.Path != "/" {
		http.NotFound(writer, request)
		return
	}
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(writer, indexHTML)
}

func (server *Server) statusAPI(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	snapshot, err := server.Snapshot()
	if err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(writer, snapshot)
}

func (server *Server) runAPI(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := server.RunNow(); err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "已在运行") {
			status = http.StatusConflict
		}
		http.Error(writer, err.Error(), status)
		return
	}
	writeJSON(writer, map[string]any{"ok": true, "message": "已启动一次同步"})
}

func (server *Server) configAPI(writer http.ResponseWriter, request *http.Request) {
	switch request.Method {
	case http.MethodGet:
		document, err := LoadConfigurationDocument(server.ConfigPath)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(writer, document)
	case http.MethodPut:
		request.Body = http.MaxBytesReader(writer, request.Body, 1024*1024)
		var document ConfigurationDocument
		decoder := json.NewDecoder(request.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&document); err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		if err := server.SaveConfiguration(document); err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(writer, map[string]any{"ok": true, "message": "配置已保存"})
	default:
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (server *Server) logsAPI(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	limit, _ := strconv.Atoi(request.URL.Query().Get("limit"))
	document, err := LoadLogs(server.ConfigPath, limit)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(writer, document)
}

func (server *Server) folderAPI(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	path, err := chooseFolder()
	if err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(writer, map[string]string{"path": filepath.Clean(path)})
}

func writeJSON(writer http.ResponseWriter, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(value)
}

func randomToken() (string, error) {
	buffer := make([]byte, 24)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}
