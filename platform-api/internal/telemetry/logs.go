package telemetry

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// LogFile keeps at most five immutable rotated files plus the active 5 MiB file.
// Only operational fields are accepted; request bodies, query strings and headers never enter this stream.
type LogFile struct {
	mu                  sync.Mutex
	directory, instance string
	file                *os.File
	size                int64
}

func NewLogFile(directory, instance string) (*LogFile, error) {
	if e := os.MkdirAll(directory, 0700); e != nil {
		return nil, e
	}
	l := &LogFile{directory: directory, instance: instance}
	e := l.open()
	return l, e
}
func (l *LogFile) open() error {
	f, e := os.OpenFile(filepath.Join(l.directory, "api.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if e != nil {
		return e
	}
	s, e := f.Stat()
	if e != nil {
		f.Close()
		return e
	}
	l.file = f
	l.size = s.Size()
	return nil
}
func (l *LogFile) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil {
		return l.file.Close()
	}
	return nil
}
func logRoute(path string) string {
	for _, prefix := range []string{"/api/applications", "/api/teams", "/api/admin", "/api/team-invitations"} {
		if path == prefix {
			return prefix
		}
		if strings.HasPrefix(path, prefix+"/") {
			return prefix + "/{resource}"
		}
	}
	for _, route := range []string{"/api/health", "/api/auth/login", "/api/auth/logout", "/api/auth/me", "/api/auth/password", "/api/auth/logout-all", "/api/auth/forgot-password", "/api/auth/reset-password", "/api/auth/recovery"} {
		if path == route {
			return route
		}
	}
	return "unmatched"
}
func (l *LogFile) Record(method, path string, status int, duration time.Duration) error {
	if strings.HasPrefix(path, "/api/operations") || strings.HasPrefix(path, "/internal/") {
		return nil
	}
	switch method {
	case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS":
	default:
		method = "OTHER"
	}
	level := "info"
	if status >= 500 {
		level = "error"
	} else if status >= 400 {
		level = "warn"
	}
	data, _ := json.Marshal(map[string]any{"time": time.Now().UTC().Format(time.RFC3339Nano), "app": "mini-fury", "instance": l.instance, "event": "http_request", "level": level, "method": method, "route": logRoute(path), "status": status, "duration_ms": duration.Milliseconds()})
	data = append(data, '\n')
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.size+int64(len(data)) > 5*1024*1024 {
		if e := l.file.Close(); e != nil {
			return e
		}
		if e := os.Rename(filepath.Join(l.directory, "api.log"), filepath.Join(l.directory, fmt.Sprintf("api-%d.log", time.Now().UnixNano()))); e != nil {
			l.open()
			return e
		}
		if e := l.open(); e != nil {
			return e
		}
		names, _ := filepath.Glob(filepath.Join(l.directory, "api-*.log"))
		sort.Strings(names)
		for len(names) > 5 {
			os.Remove(names[0])
			names = names[1:]
		}
	}
	n, e := l.file.Write(data)
	l.size += int64(n)
	return e
}
