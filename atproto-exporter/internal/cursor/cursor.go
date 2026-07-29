// Package cursor persists small source cursors (the Jetstream time_us and the
// PLC export "after" token) to disk with atomic writes, so the exporter can
// resume where it left off after a restart. Writes go to a temp file in the same
// directory and are renamed into place, so a crash never leaves a torn cursor.
package cursor

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Store reads and writes a single cursor value at a fixed path.
type Store struct {
	path string
}

// NewStore returns a Store backed by the file at path. Parent directories are
// created on first write.
func NewStore(path string) *Store {
	return &Store{path: path}
}

// Read returns the stored cursor, or "" if the file does not exist yet.
func (s *Store) Read() (string, error) {
	b, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

// Write atomically persists v: it writes to a temp file in the same directory,
// fsyncs it, then renames it over the target.
func (s *Store) Write(v string) error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".cursor-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after a successful rename

	if _, err := tmp.WriteString(v); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, s.path)
}

// ReadInt64 returns the stored cursor parsed as int64, or def if the file is
// missing or empty.
func (s *Store) ReadInt64(def int64) (int64, error) {
	v, err := s.Read()
	if err != nil {
		return 0, err
	}
	if v == "" {
		return def, nil
	}
	return strconv.ParseInt(v, 10, 64)
}

// WriteInt64 atomically persists an int64 cursor.
func (s *Store) WriteInt64(v int64) error {
	return s.Write(strconv.FormatInt(v, 10))
}

// RewindMicros subtracts rewind from a unix-microsecond cursor, clamped at 0.
// A zero cursor (fresh start) stays zero so we don't request negative time.
func RewindMicros(timeUS int64, rewind time.Duration) int64 {
	if timeUS <= 0 {
		return 0
	}
	out := timeUS - rewind.Microseconds()
	if out < 0 {
		return 0
	}
	return out
}
