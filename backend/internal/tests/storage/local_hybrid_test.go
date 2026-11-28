package storage_test

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"video-canvas/internal/config"
	"video-canvas/internal/storage"
)

func TestMirrorMatchesRevalidatesReplacementAndModification(t *testing.T) {
	dir := t.TempDir()
	local, err := storage.NewLocal(config.LocalStorage{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	const key = "video.mp4"
	write := func(data string) {
		t.Helper()
		if err := local.Put(t.Context(), key, strings.NewReader(data), int64(len(data)), ""); err != nil {
			t.Fatal(err)
		}
	}
	match := func() bool {
		return local.MirrorMatches(t.Context(), key, 4, fmt.Sprintf("%x", sha256.Sum256([]byte("good"))))
	}
	setTime := func(tstamp time.Time) {
		t.Helper()
		path := filepath.Join(dir, key)
		if err := os.Chtimes(path, tstamp, tstamp); err != nil {
			t.Fatal(err)
		}
	}
	initialTime := time.Now().Add(-time.Hour).Truncate(time.Second)
	write("good")
	setTime(initialTime)
	for range 2 {
		if !match() {
			t.Fatal("内容一致的镜像应可复用")
		}
	}
	write("evil")
	setTime(initialTime)
	if match() {
		t.Fatal("替换成同大小、同修改时间的文件后必须重新验证")
	}
	if err := os.WriteFile(filepath.Join(dir, key), []byte("good"), 0o600); err != nil {
		t.Fatal(err)
	}
	setTime(initialTime.Add(time.Minute))
	if !match() {
		t.Fatal("原地修改后必须重新验证")
	}
}

func BenchmarkMirrorMatchesRepeatedVideoReads(b *testing.B) {
	local, err := storage.NewLocal(config.LocalStorage{Dir: b.TempDir()})
	if err != nil {
		b.Fatal(err)
	}
	const key = "video.mp4"
	data := strings.Repeat("a", 16<<20)
	if err := local.Put(b.Context(), key, strings.NewReader(data), int64(len(data)), ""); err != nil {
		b.Fatal(err)
	}
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(data)))
	if !local.MirrorMatches(b.Context(), key, int64(len(data)), digest) {
		b.Fatal("镜像校验失败")
	}
	b.ResetTimer()
	for b.Loop() {
		if !local.MirrorMatches(b.Context(), key, int64(len(data)), digest) {
			b.Fatal("镜像校验失败")
		}
	}
}
