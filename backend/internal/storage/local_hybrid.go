package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

// MirrorSize 读取可安全用于镜像的本地普通文件大小。
func (l *LocalStorage) MirrorSize(key string) (int64, error) {
	if err := ValidateKey(key); err != nil {
		return 0, err
	}
	root, err := os.OpenRoot(l.dir)
	if err != nil {
		return 0, err
	}
	defer root.Close()
	info, err := root.Stat(filepath.FromSlash(key))
	if errors.Is(err, fs.ErrNotExist) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	if !info.Mode().IsRegular() {
		return 0, errors.New("本地同路径对象不是普通文件")
	}
	return info.Size(), nil
}

// PutIfMissing 在存储根目录内写完文件后原子发布，已有同路径文件时保持原样。
func (l *LocalStorage) PutIfMissing(ctx context.Context, key string, r io.Reader, size int64) error {
	if size < 0 || size == math.MaxInt64 {
		return errors.New("本地镜像对象大小不合法")
	}
	if err := ValidateKey(key); err != nil {
		return err
	}
	root, err := os.OpenRoot(l.dir)
	if err != nil {
		return err
	}
	defer root.Close()
	target := filepath.FromSlash(key)
	if err := root.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	temp := filepath.Join(filepath.Dir(target), ".hybrid-"+uuid.NewString())
	file, err := root.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = root.Remove(temp) }()
	if err := writeMirrorFile(ctx, file, r, size); err != nil {
		return err
	}
	if err := root.Link(temp, target); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return ErrObjectExists
		}
		return fmt.Errorf("无法原子发布本地镜像: %w", err)
	}
	return nil
}

func writeMirrorFile(ctx context.Context, file *os.File, r io.Reader, size int64) error {
	wrote, err := io.Copy(file, io.LimitReader(&ctxReader{ctx: ctx, r: r}, size+1))
	if err == nil && wrote == size {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if wrote != size {
		return io.ErrUnexpectedEOF
	}
	return closeErr
}

// MirrorMatches 核验文件字节与原素材记录一致，防止同路径的不同文件被当成原素材。
func (l *LocalStorage) MirrorMatches(ctx context.Context, key string, size int64, digest string) bool {
	if ValidateKey(key) != nil || len(digest) != sha256.Size*2 {
		return false
	}
	root, err := os.OpenRoot(l.dir)
	if err != nil {
		return false
	}
	defer root.Close()
	file, err := root.Open(filepath.FromSlash(key))
	if err != nil {
		return false
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != size {
		return false
	}
	hash := sha256.New()
	n, err := io.Copy(hash, &ctxReader{ctx: ctx, r: file})
	return err == nil && n == size && strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), digest)
}
