package logx

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// failWriter stands in for the invalid stderr handle a Windows service gets
// from the SCM: every write fails.
type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("invalid handle") }

func TestInitFileEmptyPathIsNoop(t *testing.T) {
	c, err := InitFile("")
	if err != nil {
		t.Fatalf("InitFile(\"\"): %v", err)
	}
	if c != nil {
		t.Fatalf("InitFile(\"\") returned a closer; want nil")
	}
}

func TestInitFileWritesToFile(t *testing.T) {
	old := log.Writer()
	t.Cleanup(func() { log.SetOutput(old) })

	// The parent directory does not exist yet: InitFile has to create it.
	path := filepath.Join(t.TempDir(), "logs", "gateway.log")
	c, err := InitFile(path)
	if err != nil {
		t.Fatalf("InitFile: %v", err)
	}
	defer func() { _ = c.Close() }()

	log.Print("hello")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.Contains(string(data), "hello") {
		t.Fatalf("log file %q does not contain the line: %q", path, data)
	}
}

// The console mirror must never be able to suppress the file write.
func TestTeeSurvivesConsoleFailure(t *testing.T) {
	var file bytes.Buffer
	tw := &tee{file: &file, console: failWriter{}}

	if _, err := tw.Write([]byte("hello\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got := file.String(); got != "hello\n" {
		t.Fatalf("file got %q; want %q", got, "hello\n")
	}
}

func TestRotatingWriterRotatesAndKeepsAllLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gateway.log")
	// 10-byte lines against a 32-byte cap: every fourth write rotates.
	w := &rotatingWriter{path: path, maxBytes: 32, keep: 2}
	defer func() { _ = w.Close() }()

	const writes = 10
	for i := 0; i < writes; i++ {
		if _, err := w.Write([]byte(fmt.Sprintf("%09d\n", i))); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}

	// keep=2 means .1 and .2 exist but nothing older does.
	if _, err := os.Stat(path + ".3"); !os.IsNotExist(err) {
		t.Fatalf("%s.3 exists (stat err = %v); keep=2 should have dropped it", path, err)
	}

	// Oldest first. The newest writes must survive intact and in order; only
	// the oldest are allowed to fall off the end.
	var got []string
	for _, p := range []string{path + ".2", path + ".1", path} {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		// A rotation that split a line would leave a size that is not a whole
		// number of records.
		if len(data)%10 != 0 {
			t.Fatalf("%s is %d bytes, not a whole number of 10-byte lines: %q", p, len(data), data)
		}
		got = append(got, strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")...)
	}

	// 3 lines fit per file and there are 3 files, so lines 0..2 have aged out
	// and 3..9 must remain, in order.
	want := make([]string, 0, 7)
	for i := 3; i < writes; i++ {
		want = append(want, fmt.Sprintf("%09d", i))
	}
	if len(got) != len(want) {
		t.Fatalf("retained %d lines (%v); want %d (%v)", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("line %d = %q; want %q (full: %v)", i, got[i], want[i], got)
		}
	}
}

// A single write larger than the cap must not rotate an empty file into a
// backup, which would leave the live log missing its first line.
func TestRotatingWriterDoesNotRotateEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gateway.log")
	w := &rotatingWriter{path: path, maxBytes: 4, keep: 2}
	defer func() { _ = w.Close() }()

	if _, err := w.Write([]byte("oversized line\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, err := os.Stat(path + ".1"); !os.IsNotExist(err) {
		t.Fatalf("an empty file was rotated into a backup (stat err = %v)", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if got := string(data); got != "oversized line\n" {
		t.Fatalf("live log got %q; want %q", got, "oversized line\n")
	}
}
