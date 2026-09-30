package plugin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"video-canvas/internal/provider"
	"video-canvas/internal/provider/pluginproto"
)

// CodeStore 按插件版本取代码，runner 不认识某个 sha256（重启过）时宿主用它重新装载。由 service 层适配版本表实现。
type CodeStore interface {
	// Code 返回版本 versionID 的插件代码；sha256 是快照里冻结的哈希，实现方可以用它做额外核对。
	Code(ctx context.Context, versionID uint64, sha256 string) (string, error)
}

// loader 负责“runner 不认识插件就装载”：同一个 sha256 并发装载只做一次（singleflight 式去重），
// 并缓存装载时 runner 报告的钩子列表，之后可以不经 runner 就知道某个可选钩子不存在。
type loader struct {
	runner RunnerClient
	codes  CodeStore

	mu       sync.Mutex
	inflight map[string]*loadCall
	hooks    map[string]map[string]bool
}

// loadCall 是一次进行中的装载。
type loadCall struct {
	done chan struct{}
	err  error
}

func newLoader(runner RunnerClient, codes CodeStore) *loader {
	return &loader{runner: runner, codes: codes, inflight: map[string]*loadCall{}, hooks: map[string]map[string]bool{}}
}

// mayHave 判断插件可能导出了 hook：装载过且钩子列表里没有才返回 false，不知道时按“可能有”处理（交给 runner 判断）。
func (l *loader) mayHave(sha, hook string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	set, ok := l.hooks[sha]
	return !ok || set[hook]
}

// call 调用钩子；runner 回答 unknown_plugin 时装载代码后重试同一个调用一次。
func (l *loader) call(ctx context.Context, rt *provider.ChannelRuntime, hook string, args []json.RawMessage, timeout time.Duration) (*pluginproto.CallResponse, error) {
	sha := rt.Plugin.SHA256
	resp, err := l.runner.Call(ctx, sha, hook, args, timeout)
	if err != nil || resp.Error == nil || resp.Error.Code != pluginproto.CodeUnknownPlugin {
		return resp, err
	}
	if err := l.load(ctx, rt.Channel.PluginVersionID, sha); err != nil {
		return nil, err
	}
	return l.runner.Call(ctx, sha, hook, args, timeout)
}

// load 装载一个插件版本；同一 sha256 同时只有一个装载在进行，其余调用方等它的结果（或自己的 ctx 结束）。
func (l *loader) load(ctx context.Context, versionID uint64, sha string) error {
	l.mu.Lock()
	if c, ok := l.inflight[sha]; ok {
		l.mu.Unlock()
		select {
		case <-c.done:
			return c.err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	c := &loadCall{done: make(chan struct{})}
	l.inflight[sha] = c
	l.mu.Unlock()

	c.err = l.doLoad(ctx, versionID, sha)

	l.mu.Lock()
	delete(l.inflight, sha)
	l.mu.Unlock()
	close(c.done)
	return c.err
}

// errLoadFailed 表示 runner 装载插件失败（已登记的版本本不应失败，预检保证过）。
var errLoadFailed = errors.New("装载插件失败")

// doLoad 取代码、核对哈希、交给 runner 装载，并记下 runner 报告的钩子列表。
func (l *loader) doLoad(ctx context.Context, versionID uint64, sha string) error {
	if l.codes == nil {
		return terminalErr(codeHostMisconfigured, "宿主未配置插件代码仓库，无法装载插件", nil)
	}
	code, err := l.codes.Code(ctx, versionID, sha)
	if err != nil {
		return retryableErr("", "读取插件代码失败", err)
	}
	// 代码与快照里冻结的哈希不一致，说明版本表被改过或取错了版本：拒绝装载，宁可失败也不跑“不是这个版本”的代码
	sum := sha256.Sum256([]byte(code))
	if hex.EncodeToString(sum[:]) != sha {
		return terminalErr(provider.CodePluginError, "插件代码与快照中的 sha256 不一致，拒绝装载", nil)
	}
	resp, err := l.runner.Load(ctx, sha, code)
	if err != nil {
		return err
	}
	if !resp.OK {
		msg := "runner 未说明原因"
		if resp.Error != nil {
			msg = resp.Error.Code + "：" + resp.Error.Message
		}
		return &provider.Error{Class: provider.ClassTerminal, Code: provider.CodePluginError, Message: "装载插件失败：" + msg, Cause: errLoadFailed, PluginFault: true}
	}
	if resp.Hooks != nil {
		set := make(map[string]bool, len(resp.Hooks))
		for _, h := range resp.Hooks {
			set[h] = true
		}
		l.mu.Lock()
		l.hooks[sha] = set
		l.mu.Unlock()
	}
	return nil
}
