package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"os"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

const (
	mirrorCheckMax = 4096
	mirrorCheckTTL = 10 * time.Minute
)

type mirrorCheck struct {
	info    fs.FileInfo
	digest  string
	expires time.Time
	matches bool
}

// mirrorChecks 避免视频的连续 Range 请求重复扫描整个 NAS 文件。
type mirrorChecks struct {
	mu     sync.Mutex
	items  map[string]mirrorCheck
	checks singleflight.Group
}

func (c *mirrorChecks) matches(ctx context.Context, key string, file *os.File, info fs.FileInfo, digest string) bool {
	if check, ok := c.lookup(key, info, digest); ok {
		return check.matches
	}
	value, _, _ := c.checks.Do(key+"\x00"+digest, func() (any, error) {
		if check, ok := c.lookup(key, info, digest); ok {
			return check, nil
		}
		check, ok := checkMirrorContent(ctx, file, info, digest)
		if ok {
			c.remember(key, check)
		}
		return check, nil
	})
	check := value.(mirrorCheck)
	// 合并中的请求可能打开了刚被替换的文件，只接受同一份文件的校验结果。
	return check.matches && sameMirrorFile(info, check.info)
}

func (c *mirrorChecks) lookup(key string, info fs.FileInfo, digest string) (mirrorCheck, bool) {
	c.mu.Lock()
	check := c.items[key]
	c.mu.Unlock()
	ok := check.digest == digest && time.Now().Before(check.expires) && sameMirrorFile(info, check.info)
	return check, ok
}

func (c *mirrorChecks) remember(key string, check mirrorCheck) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.items == nil {
		c.items = make(map[string]mirrorCheck)
	}
	if _, exists := c.items[key]; !exists && len(c.items) >= mirrorCheckMax {
		for victim := range c.items {
			delete(c.items, victim)
			break
		}
	}
	c.items[key] = check
}

func checkMirrorContent(ctx context.Context, file *os.File, info fs.FileInfo, digest string) (mirrorCheck, bool) {
	hash := sha256.New()
	n, err := io.Copy(hash, &ctxReader{ctx: ctx, r: file})
	if err != nil || n != info.Size() {
		return mirrorCheck{}, false
	}
	after, err := file.Stat()
	if err != nil || !sameMirrorFile(info, after) {
		return mirrorCheck{}, false
	}
	return mirrorCheck{
		info: after, digest: digest, expires: time.Now().Add(mirrorCheckTTL),
		matches: hex.EncodeToString(hash.Sum(nil)) == digest,
	}, true
}

func sameMirrorFile(a, b fs.FileInfo) bool {
	return b != nil && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime()) && os.SameFile(a, b)
}
