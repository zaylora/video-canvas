package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/config"
)

// filesRoutePrefix 是本地存储静态路由的对外前缀，URL() 与 FileServer 必须一致。
const filesRoutePrefix = "/files/"

// LocalStorage 把对象写到本地磁盘，仅用于开发环境：没有签名、没有过期，
// 文件通过 /files/*filepath 静态路由公开访问（对象 key 不可猜测，但不要用于生产）。
type LocalStorage struct {
	dir     string // 存放目录的绝对路径
	baseURL string // 对外访问前缀（已去掉末尾 /），为空表示相对路径
}

// NewLocal 创建本地存储，目录不存在时自动创建。
func NewLocal(cfg config.LocalStorage) (*LocalStorage, error) {
	dir := cfg.Dir
	if dir == "" {
		return nil, errors.New("storage: local.dir 不能为空")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("storage: 解析本地目录失败: %w", err)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, fmt.Errorf("storage: 创建本地目录失败: %w", err)
	}
	return &LocalStorage{dir: abs, baseURL: strings.TrimRight(cfg.BaseURL, "/")}, nil
}

// path 把 key 映射成磁盘路径：先校验 key，再确认结果仍在存储目录内（双重防穿越）。
func (l *LocalStorage) path(key string) (string, error) {
	if err := ValidateKey(key); err != nil {
		return "", err
	}
	full := filepath.Join(l.dir, filepath.FromSlash(key))
	rel, err := filepath.Rel(l.dir, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("%w: 超出存储目录", ErrInvalidKey)
	}
	return full, nil
}

// Put 写入对象：先写同目录下的临时文件，成功后 rename 到目标路径，
// 保证读取方永远看不到写了一半的文件；任何失败都会清理临时文件。
// contentType 本地存储不保存（读取时按扩展名推断）。
func (l *LocalStorage) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) (err error) {
	full, err := l.path(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return fmt.Errorf("storage: 创建目录失败: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(full), ".tmp-*")
	if err != nil {
		return fmt.Errorf("storage: 创建临时文件失败: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
		}
	}()

	n, err := io.Copy(tmp, &ctxReader{ctx: ctx, r: r})
	if err != nil {
		return fmt.Errorf("storage: 写入失败: %w", err)
	}
	if size >= 0 && n != size {
		err = fmt.Errorf("storage: 写入长度 %d 与声明长度 %d 不一致: %w", n, size, io.ErrUnexpectedEOF)
		return err
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("storage: 关闭临时文件失败: %w", err)
	}
	if err = os.Rename(tmpName, full); err != nil {
		return fmt.Errorf("storage: 重命名失败: %w", err)
	}
	return nil
}

// Open 读取对象，不存在返回 ErrNotFound。
func (l *LocalStorage) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	f, _, err := l.openFile(key)
	if err != nil {
		return nil, err
	}
	return f, nil
}

// openFile 打开磁盘文件并返回 stat 信息，目录和非法 key 都视为不存在。
func (l *LocalStorage) openFile(key string) (*os.File, fs.FileInfo, error) {
	full, err := l.path(key)
	if err != nil {
		// 非法 key 不可能对应任何对象，对读取方等价于不存在
		return nil, nil, ErrNotFound
	}
	f, err := os.Open(full)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, fmt.Errorf("storage: 打开文件失败: %w", err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, nil, fmt.Errorf("storage: 读取文件信息失败: %w", err)
	}
	if info.IsDir() {
		_ = f.Close()
		return nil, nil, ErrNotFound
	}
	return f, info, nil
}

// URL 返回静态路由地址：BaseURL + /files/ + key，BaseURL 为空时是相对路径。ttl 对本地存储无意义。
func (l *LocalStorage) URL(ctx context.Context, key string, ttl time.Duration) (string, error) {
	if err := ValidateKey(key); err != nil {
		return "", err
	}
	return l.baseURL + filesRoutePrefix + key, nil
}

// Delete 删除对象，不存在不算错误。
func (l *LocalStorage) Delete(ctx context.Context, key string) error {
	full, err := l.path(key)
	if err != nil {
		return err
	}
	if err := os.Remove(full); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("storage: 删除失败: %w", err)
	}
	return nil
}

// FileServer 返回本地存储的静态文件处理器，挂在 /files/*filepath（GET 与 HEAD）。
//
// 用 http.ServeContent 提供 Range / If-Modified-Since 支持，视频可拖动播放；
// key 校验失败或文件不存在统一 404；对象内容不可变，所以设置长缓存。
func FileServer(l *LocalStorage) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := strings.TrimPrefix(c.Param("filepath"), "/")
		f, info, err := l.openFile(key)
		if err != nil {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		defer f.Close()

		h := c.Writer.Header()
		if ct := mime.TypeByExtension(filepath.Ext(key)); ct != "" {
			h.Set("Content-Type", ct)
		}
		// 禁止浏览器嗅探成可执行类型；key 不可猜测且内容不可变，可以长期缓存
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Cache-Control", "public, max-age=31536000, immutable")
		http.ServeContent(c.Writer, c.Request, filepath.Base(key), info.ModTime(), f)
	}
}

// ctxReader 在每次读取前检查 ctx，让取消的请求能及时中断写入。
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c *ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}
