package cursor

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReadMissingReturnsEmpty(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "nope.cursor"))
	v, err := s.Read()
	if err != nil {
		t.Fatalf("Read missing: %v", err)
	}
	if v != "" {
		t.Errorf("Read missing = %q, want empty", v)
	}
}

func TestWriteReadRoundTrip(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "sub", "dir", "c.cursor"))
	if err := s.Write("did:plc:abc123"); err != nil {
		t.Fatalf("Write: %v", err)
	}
	v, err := s.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if v != "did:plc:abc123" {
		t.Errorf("round-trip = %q", v)
	}
}

func TestInt64RoundTrip(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "j.cursor"))
	const want int64 = 1_726_000_000_123_456
	if err := s.WriteInt64(want); err != nil {
		t.Fatalf("WriteInt64: %v", err)
	}
	got, err := s.ReadInt64(-1)
	if err != nil {
		t.Fatalf("ReadInt64: %v", err)
	}
	if got != want {
		t.Errorf("ReadInt64 = %d, want %d", got, want)
	}
}

func TestReadInt64MissingReturnsDefault(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "j.cursor"))
	got, err := s.ReadInt64(42)
	if err != nil {
		t.Fatalf("ReadInt64: %v", err)
	}
	if got != 42 {
		t.Errorf("ReadInt64 missing = %d, want default 42", got)
	}
}

func TestWriteIsAtomicNoTempLeft(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(filepath.Join(dir, "c.cursor"))
	if err := s.Write("v1"); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := s.Write("v2"); err != nil {
		t.Fatalf("Write: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("dir has %d entries, want 1 (no temp files left): %v", len(entries), entries)
	}
	v, _ := s.Read()
	if v != "v2" {
		t.Errorf("after overwrite = %q, want v2", v)
	}
}

func TestRewindMicros(t *testing.T) {
	const now int64 = 1_000_000 // 1s in micros
	if got := RewindMicros(now, time.Second); got != 0 {
		t.Errorf("rewind 1s from 1s = %d, want 0", got)
	}
	if got := RewindMicros(now, 250*time.Millisecond); got != 750_000 {
		t.Errorf("rewind 250ms = %d, want 750000", got)
	}
	// Clamp: never negative.
	if got := RewindMicros(now, time.Hour); got != 0 {
		t.Errorf("over-rewind = %d, want clamped 0", got)
	}
	// Zero cursor stays zero (fresh start).
	if got := RewindMicros(0, 5*time.Second); got != 0 {
		t.Errorf("rewind from 0 = %d, want 0", got)
	}
}
