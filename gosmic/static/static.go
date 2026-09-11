package static

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"net/http"
)

func generateFileHash(r io.Reader) (string, error) {
	hash := sha256.New()
	_, err := io.Copy(hash, r)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)[0:8]), nil
}

type neuteredFileSystem struct {
	fs http.FileSystem
}

func (nfs neuteredFileSystem) Open(name string) (http.File, error) {
	f, err := nfs.fs.Open(name)
	if err != nil {
		return nil, err
	}

	s, err := f.Stat()
	if err != nil {
		return nil, err
	}

	if s.IsDir() {
		return nil, fs.ErrNotExist
	}

	return f, nil
}

type StaticFS struct {
	resources fs.FS
}

func NewStaticFS(resources fs.FS) *StaticFS {
	return &StaticFS{resources: resources}
}

func (s *StaticFS) FileHash(name string) string {
	file, err := s.resources.Open(name)
	if err != nil {
		panic(fmt.Errorf("opening %s for hashing: %w", name, err))
	}
	defer file.Close()

	hash, err := generateFileHash(file)
	if err != nil {
		panic(fmt.Errorf("hashing %s: %w", name, err))
	}
	return hash
}

func (s *StaticFS) Handler() http.Handler {
	fs := http.FS(s.resources)
	h := http.FileServer(neuteredFileSystem{fs})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Has("v") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		h.ServeHTTP(w, r)
	})
}
