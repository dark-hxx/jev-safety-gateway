package logx

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
)

// Rotation defaults for the file named by JEV_LOG_FILE. A Windows service is
// the reason this exists: the SCM gives the process no console, so stdout and
// stderr are discarded and the file is the only record. It must therefore not
// grow without bound. On Linux (journald) and in Docker (json-file driver,
// capped in docker-compose.yml) JEV_LOG_FILE is normally left unset.
const (
	logMaxBytes = 10 << 20 // 10 MiB
	logKeep     = 3        // gateway.log.1 .. gateway.log.3
)

// InitFile sends the standard logger's output to path as well as to stderr,
// rotating at logMaxBytes. An empty path is a no-op, so callers can pass
// os.Getenv("JEV_LOG_FILE") unconditionally.
//
// The returned closer is for tests; a process normally leaves the file open for
// its whole lifetime.
func InitFile(path string) (io.Closer, error) {
	if path == "" {
		return nil, nil
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create log dir: %w", err)
		}
	}
	w := &rotatingWriter{path: path, maxBytes: logMaxBytes, keep: logKeep}
	if err := w.open(); err != nil {
		return nil, err
	}
	log.SetOutput(&tee{file: w, console: os.Stderr})
	return w, nil
}

// tee writes to the log file and mirrors to stderr on a best-effort basis. It
// deliberately does not use io.MultiWriter: that stops at the first error, and
// under the Windows SCM stderr is an invalid handle whose write error would
// then swallow every line before it reached the file.
type tee struct {
	file    io.Writer
	console io.Writer
}

func (t *tee) Write(p []byte) (int, error) {
	n, err := t.file.Write(p)
	_, _ = t.console.Write(p)
	return n, err
}

// rotatingWriter renames the log to <path>.1 once a write would push it past
// maxBytes, shifting older copies up and dropping the oldest beyond keep.
type rotatingWriter struct {
	mu       sync.Mutex
	path     string
	maxBytes int64
	keep     int

	f    *os.File
	size int64
}

func (w *rotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.f == nil {
		if err := w.open(); err != nil {
			return 0, err
		}
	}
	// size > 0 keeps a single oversized line from rotating an empty file.
	if w.size > 0 && w.size+int64(len(p)) > w.maxBytes {
		if err := w.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := w.f.Write(p)
	w.size += int64(n)
	return n, err
}

func (w *rotatingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return nil
	}
	err := w.f.Close()
	w.f = nil
	return err
}

func (w *rotatingWriter) open() error {
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return err
	}
	w.f, w.size = f, info.Size()
	return nil
}

func (w *rotatingWriter) rotate() error {
	if err := w.f.Close(); err != nil {
		w.f = nil
		return err
	}
	w.f = nil
	for i := w.keep - 1; i >= 1; i-- {
		if err := os.Rename(w.backup(i), w.backup(i+1)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if err := os.Rename(w.path, w.backup(1)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return w.open()
}

func (w *rotatingWriter) backup(n int) string { return fmt.Sprintf("%s.%d", w.path, n) }
