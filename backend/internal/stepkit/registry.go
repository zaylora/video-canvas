package stepkit

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// Registry 是环节注册表：启动时由 initialize 创建并注册各业务包的环节，之后只读。
// 它是普通对象而不是包级单例（后端 A4：依赖只在 initialize 里手动组装），测试可以各建各的。
type Registry struct {
	mu    sync.RWMutex
	steps []Step
	index map[string]int
}

// NewRegistry 创建空注册表。
func NewRegistry() *Registry { return &Registry{index: map[string]int{}} }

// Register 登记一个环节。key 为空或重复、缺职责说明、缺校验函数、Schema 不是合法 JSON、媒体需求未知都会拒绝，
// 让协议缺陷在启动时暴露，而不是运行到那一步才出错。
func (r *Registry) Register(s Step) error {
	if err := checkStep(s); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.index[s.Key]; dup {
		return fmt.Errorf("环节 %s 重复注册", s.Key)
	}
	r.index[s.Key] = len(r.steps)
	r.steps = append(r.steps, s)
	return nil
}

// MustRegister 同 Register，失败直接 panic，用于启动时登记内置环节（定义错误属于不可恢复的启动错误）。
func (r *Registry) MustRegister(s Step) {
	if err := r.Register(s); err != nil {
		panic(err)
	}
}

// Get 按 key 取环节。
func (r *Registry) Get(key string) (Step, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	i, ok := r.index[key]
	if !ok {
		return Step{}, false
	}
	return r.steps[i], true
}

// List 按注册顺序返回全部环节的副本。
func (r *Registry) List() []Step {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]Step(nil), r.steps...)
}

// checkStep 检查环节定义是否完整合法。
func checkStep(s Step) error {
	if strings.TrimSpace(s.Key) == "" {
		return fmt.Errorf("环节 key 不能为空")
	}
	if strings.TrimSpace(s.Duty) == "" {
		return fmt.Errorf("环节 %s 缺少本步职责", s.Key)
	}
	if s.Validate == nil {
		return fmt.Errorf("环节 %s 缺少校验函数", s.Key)
	}
	if !s.Media.Valid() {
		return fmt.Errorf("环节 %s 的媒体需求 %q 未知", s.Key, s.Media)
	}
	var schema map[string]any
	if err := json.Unmarshal(s.Schema, &schema); err != nil {
		return fmt.Errorf("环节 %s 的 Schema 不是合法 JSON 对象：%w", s.Key, err)
	}
	return nil
}
