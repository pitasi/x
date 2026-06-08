package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/bep/imagemeta"
	"github.com/davidbyttow/govips/v2/vips"
)

func safeMove(old, new string) error {
	if fileExists(new) {
		return fmt.Errorf("destination file already exists: %s", new)
	}
	if err := os.MkdirAll(filepath.Dir(new), 0766); err != nil {
		return err
	}
	return os.Rename(old, new)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil

}

func w(src *vips.ImageRef, width int, outPath string) {
	outName := filepath.Join(outPath, fmt.Sprintf("w_%d.webp", width))
	if fileExists(outName) {
		return
	}

	image, err := src.Copy()
	if err != nil {
		panic(err)
	}

	newHeight := src.Height() * width / src.Width()
	if err := image.Thumbnail(width, newHeight, vips.InterestingAll); err != nil {
		panic(err)
	}

	params := vips.NewWebpExportParams()
	params.Quality = 80
	params.StripMetadata = true
	content, _, err := image.ExportWebp(params)
	if err != nil {
		panic(err)
	}

	if err := os.WriteFile(outName, content, 0644); err != nil {
		panic(err)
	}
	log.Println("written", outName)
}

func thumbnail(src *vips.ImageRef, size int, outPath string) {
	outName := filepath.Join(outPath, fmt.Sprintf("q_%d.webp", size))
	if fileExists(outName) {
		return
	}

	image, err := src.Copy()
	if err != nil {
		panic(err)
	}

	if err := image.Thumbnail(size, size, vips.InterestingCentre); err != nil {
		panic(err)
	}

	params := vips.NewWebpExportParams()
	params.Quality = 10
	params.StripMetadata = true
	content, _, err := image.ExportWebp(params)
	if err != nil {
		panic(err)
	}

	if err := os.WriteFile(outName, content, 0644); err != nil {
		panic(err)
	}
	log.Println("written", outName)
}

func hashjpg(content []byte) []byte {
	if !bytes.Equal(content[0:2], []byte("\xff\xd8")) {
		panic("invalid jpeg header")
	}

	h := sha256.New()

	start := 2
	for start < len(content) {
		fmt.Printf("%x\n", content[start:start+32])
		marker1 := content[start]
		if marker1 != 0xFF {
			panic("invalid jpeg content")
		}
		start += 1

		marker2 := content[start]
		start += 1

		length := (int(content[start]) << 8) | int(content[start+1])
		fmt.Println("length", length)

		if marker2 == 0xDA {
			if _, err := h.Write(content[start+2 : start+length]); err != nil {
				panic(err)
			}
		}

		start += length
	}

	return h.Sum(nil)
}

type set struct {
	mu sync.Mutex
	m  map[string]struct{}
}

func NewSet() *set { return &set{m: make(map[string]struct{})} }

func (s *set) add(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[id] = struct{}{}
}
func (s *set) has(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, found := s.m[id]
	return found
}

var written = NewSet()

func process(photodb, path string) {
	content, err := os.ReadFile(path)
	if err != nil {
		panic(err)
	}

	image, err := vips.NewImageFromBuffer(content)
	if err != nil {
		panic(err)
	}

	h := sha256.New()
	if _, err := h.Write(content); err != nil {
		panic(err)
	}
	hash := h.Sum(nil)

	var tags imagemeta.Tags
	if err := imagemeta.Decode(imagemeta.Options{
		R:           bytes.NewReader(content),
		ImageFormat: imagemeta.JPEG,
		Sources:     imagemeta.EXIF | imagemeta.IPTC,
		HandleTag: func(info imagemeta.TagInfo) error {
			tags.Add(info)
			return nil
		},
	}); err != nil {
		panic(err)
	}
	ts, err := tags.GetDateTime()
	if err != nil {
		panic(err)
	}
	if ts.IsZero() {
		panic("image doesn't have timestamp - a timestamp is required because it's used as part of the ID")
	}
	ts = ts.UTC()

	id := fmt.Sprintf("%d-%02d-%02d_%02d:%02d:%02d_%x", ts.Year(), ts.Month(), ts.Day(), ts.Hour(), ts.Minute(), ts.Second(), hash[:2])
	defer written.add(id)

	photoPath := filepath.Join(photodb, id)
	if err := os.MkdirAll(photoPath, 0766); err != nil {
		panic(err)
	}

	infoJsonPath := filepath.Join(photoPath, "info.json")
	infoJsonFile, err := os.Create(infoJsonPath)
	if err != nil {
		panic(err)
	}

	if err := json.NewEncoder(infoJsonFile).Encode(tags.All()); err != nil {
		panic(err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		thumbnail(image, 500, photoPath)
		wg.Done()
	}()
	wg.Add(1)
	go func() {
		thumbnail(image, 1000, photoPath)
		wg.Done()
	}()
	wg.Add(1)
	go func() {
		thumbnail(image, 2000, photoPath)
		wg.Done()
	}()
	wg.Add(1)
	go func() {
		w(image, 1200, photoPath)
		wg.Done()
	}()
	wg.Add(1)
	go func() {
		w(image, 1900, photoPath)
		wg.Done()
	}()
	wg.Add(1)
	go func() {
		w(image, 2500, photoPath)
		wg.Done()
	}()
	wg.Wait()
}

func buildFolderMap(referenceDir string) (map[string]string, error) {
	res := make(map[string]string)
	inputFS := os.DirFS(referenceDir)
	err := fs.WalkDir(inputFS, ".", func(path string, d fs.DirEntry, err error) error {
		if !d.Type().IsRegular() {
			return nil
		}
		ext := filepath.Ext(d.Name())
		extLower := strings.ToLower(ext)
		if extLower != ".arw" && extLower != ".dng" {
			return nil
		}
		nameWithoutExt := strings.TrimSuffix(d.Name(), ext)
		res[nameWithoutExt] = filepath.Dir(path)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return res, nil
}

func organizeFolder(dir string, m map[string]string) error {
	inputDir := filepath.Join(dir, "_to_organize")
	inputFS := os.DirFS(inputDir)
	err := fs.WalkDir(inputFS, ".", func(path string, d fs.DirEntry, err error) error {
		if !d.Type().IsRegular() {
			return nil
		}
		ext := filepath.Ext(d.Name())
		nameWithoutExt := strings.TrimSuffix(d.Name(), ext)
		s, found := m[nameWithoutExt]
		if !found {
			log.Println("unmapped file", nameWithoutExt)
			return nil
		}

		from := filepath.Join(inputDir, path)
		to := filepath.Join(dir, s, d.Name())

		return safeMove(from, to)
	})
	if err != nil {
		return err
	}
	return nil
}

func main() {
	rawsPath := os.Args[1]
	privatePath := os.Args[2]
	inputPath := os.Args[3]
	photodbPath := os.Args[4]

	folderMap, err := buildFolderMap(rawsPath)
	if err != nil {
		panic(err)
	}

	if err := organizeFolder(privatePath, folderMap); err != nil {
		panic(err)
	}

	if err := organizeFolder(inputPath, folderMap); err != nil {
		panic(err)
	}

	if err := os.MkdirAll(photodbPath, 0766); err != nil {
		panic(err)
	}

	vips.LoggingSettings(nil, vips.LogLevelWarning)
	vips.Startup(&vips.Config{
		ConcurrencyLevel: 0,
	})
	defer vips.Shutdown()

	ins := make(chan string)
	workers := 4
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			for in := range ins {
				process(photodbPath, in)
			}
			wg.Done()
		}()
	}

	inputFS := os.DirFS(inputPath)
	if err := fs.WalkDir(inputFS, ".", func(path string, d fs.DirEntry, err error) error {
		if !d.Type().IsRegular() {
			return nil
		}

		if !strings.HasSuffix(path, ".jpg") {
			return nil
		}

		if strings.HasSuffix(path, "_ig.jpg") {
			return nil
		}

		fullPath := filepath.Join(inputPath, path)
		ins <- fullPath

		return nil
	}); err != nil {
		panic(err)
	}
	close(ins)

	wg.Wait()

	outputFS := os.DirFS(photodbPath)
	if err := fs.WalkDir(outputFS, ".", func(path string, d fs.DirEntry, err error) error {
		if path == "." || !d.IsDir() {
			return nil
		}
		if written.has(path) {
			return nil
		}
		return os.RemoveAll(filepath.Join(photodbPath, path))
	}); err != nil {
		panic(err)
	}
}
