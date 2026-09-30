package pluginrunner

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/dop251/goja"
	"github.com/google/uuid"

	"video-canvas/internal/provider/pluginproto"
)

// bootstrap 在插件代码之前执行：只声明 CommonJS 风格的导出对象和 utils 容器。
const bootstrap = `
var module = {exports: {}};
var exports = module.exports;
var utils = {};
`

// lockdown 封堵动态代码入口，必须在插件顶层代码执行之前运行：
// 插件顶层一旦先拿到 Function / eval 的引用（或 AsyncFunction 等构造器），事后再封就晚了。
// 它不是完整的安全边界；内存和网络的硬隔离依赖 runner 容器。
const lockdown = `
(function () {
  var constructors = [Object, Array, String, Number, Boolean, Function, Error, Date,
    RegExp, Symbol, Map, Set, Promise];
  for (var i = 0; i < constructors.length; i++) {
    var c = constructors[i];
    if (c && c.prototype) {
      try { Object.defineProperty(c.prototype, "constructor", {value: undefined, configurable: false}); } catch (_) {}
      Object.freeze(c.prototype);
    }
  }
  // 生成器 / 异步函数（goja 不支持异步生成器语法，插件写不出来）的原型上各有一个指向动态构造器的 constructor，逐个封掉。
  var samples = [];
  try { samples.push(function*(){}); } catch (_) {}
  try { samples.push(async function(){}); } catch (_) {}
  for (var k = 0; k < samples.length; k++) {
    var proto = Object.getPrototypeOf(samples[k]);
    try { Object.defineProperty(proto, "constructor", {value: undefined, configurable: false}); } catch (_) {}
    try { Object.freeze(proto); } catch (_) {}
  }
  for (var j = 0; j < constructors.length; j++) if (constructors[j]) Object.freeze(constructors[j]);
  Object.freeze(JSON); Object.freeze(Math); Object.freeze(Reflect);
  Object.freeze(utils);
  try { Object.defineProperty(globalThis, "eval", {value: undefined, writable: false, configurable: false}); } catch (_) {}
  try { Object.defineProperty(globalThis, "Function", {value: undefined, writable: false, configurable: false}); } catch (_) {}
})();
`

// compile 只编译插件源码；bootstrap 与 lockdown 单独编译，这样错误行号与插件文件一致。
func compile(code string) (*goja.Program, error) {
	return goja.Compile("plugin.js", code, false)
}

var (
	setupOnce    sync.Once
	setupProgram *goja.Program
	setupErr     error
)

// setup 返回 bootstrap 与 lockdown 的编译结果（进程内只编译一次）。
func setup() (boot, lock *goja.Program, err error) {
	setupOnce.Do(func() {
		setupProgram, setupErr = goja.Compile("bootstrap.js", bootstrap, false)
		if setupErr == nil {
			lockProgram, setupErr = goja.Compile("lockdown.js", lockdown, false)
		}
	})
	return setupProgram, lockProgram, setupErr
}

var lockProgram *goja.Program

// newRuntime 创建运行时并执行插件顶层代码。
// 顺序：bootstrap → utils → lockdown → 插件顶层代码 → 读取导出与钩子列表。
// 插件顶层代码和读取导出（可能触发 getter）都可能死循环，所以与钩子共用同一套超时中断（runTimed）；
// 超时返回 errTimeout，此时运行时已被中断，调用方直接丢弃。
func newRuntime(program *goja.Program, timeout time.Duration) (*runtime, error) {
	boot, lock, err := setup()
	if err != nil {
		return nil, err
	}
	vm := goja.New()
	rt := &runtime{vm: vm}
	err = runTimed(rt, timeout, func() error {
		if _, err := vm.RunProgram(boot); err != nil {
			return err
		}
		if err := installUtils(rt); err != nil {
			return err
		}
		if _, err := vm.RunProgram(lock); err != nil {
			return err
		}
		if _, err := vm.RunProgram(program); err != nil {
			return err
		}
		exports := vm.Get("module").ToObject(vm).Get("exports")
		if goja.IsUndefined(exports) || goja.IsNull(exports) {
			return errors.New("module.exports 必须是对象")
		}
		rt.exports = exports.ToObject(vm)
		rt.hooks = hooks(rt)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return rt, nil
}

func hooks(rt *runtime) []string {
	var out []string
	for _, name := range pluginproto.AllHooks {
		if _, ok := goja.AssertFunction(rt.exports.Get(name)); ok {
			out = append(out, name)
		}
	}
	return out
}

func runTimed(rt *runtime, timeout time.Duration, fn func() error) (err error) {
	expired := make(chan struct{})
	timer := time.AfterFunc(timeout, func() {
		rt.vm.Interrupt("插件执行超时")
		close(expired)
	})
	defer timer.Stop()
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("插件执行异常：%v", r)
		}
		select {
		case <-expired:
			err = errTimeout
		default:
		}
	}()
	return fn()
}

var errTimeout = errors.New("插件执行超时")

func invoke(rt *runtime, name string, args []json.RawMessage, timeout time.Duration) (json.RawMessage, error) {
	rt.logs = nil
	var result json.RawMessage
	err := runTimed(rt, timeout, func() error {
		// 读取导出属性可能触发 getter，也放在超时保护之内
		fn, ok := goja.AssertFunction(rt.exports.Get(name))
		if !ok {
			return errMissing
		}
		jsArgs := make([]goja.Value, 0, len(args))
		parse, ok := goja.AssertFunction(rt.vm.Get("JSON").ToObject(rt.vm).Get("parse"))
		if !ok {
			return errors.New("JSON.parse 不可用")
		}
		for _, raw := range args {
			v, err := parse(goja.Undefined(), rt.vm.ToValue(string(raw)))
			if err != nil {
				return err
			}
			jsArgs = append(jsArgs, v)
		}
		value, err := fn(rt.exports, jsArgs...)
		if err != nil {
			return err
		}
		if goja.IsUndefined(value) {
			result = json.RawMessage("null")
			return nil
		}
		stringify, ok := goja.AssertFunction(rt.vm.Get("JSON").ToObject(rt.vm).Get("stringify"))
		if !ok {
			return errors.New("JSON.stringify 不可用")
		}
		encoded, err := stringify(goja.Undefined(), value)
		if err != nil {
			return err
		}
		if goja.IsUndefined(encoded) {
			return errInvalidResult
		}
		result = json.RawMessage(encoded.String())
		return nil
	})
	return result, err
}

var (
	errMissing       = errors.New("插件未实现该钩子")
	errInvalidResult = errors.New("钩子返回值不能编码为 JSON")
)

const maxUtilityInput = 256 << 10

func installUtils(rt *runtime) error {
	vm := rt.vm
	utils := vm.Get("utils").ToObject(vm)
	set := func(name string, fn func(goja.FunctionCall) goja.Value) error {
		return utils.Set(name, fn)
	}
	if err := set("uuid", func(goja.FunctionCall) goja.Value { return vm.ToValue(uuid.NewString()) }); err != nil {
		return err
	}
	if err := set("unixNow", func(goja.FunctionCall) goja.Value { return vm.ToValue(time.Now().Unix()) }); err != nil {
		return err
	}
	if err := set("base64", func(call goja.FunctionCall) goja.Value {
		s := utilityString(call, 0)
		return vm.ToValue(base64.StdEncoding.EncodeToString([]byte(s)))
	}); err != nil {
		return err
	}
	if err := set("base64Decode", func(call goja.FunctionCall) goja.Value {
		s := utilityString(call, 0)
		raw, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			panic(vm.ToValue("base64 解码失败"))
		}
		return vm.ToValue(string(raw))
	}); err != nil {
		return err
	}
	if err := set("base64URL", func(call goja.FunctionCall) goja.Value {
		s := utilityString(call, 0)
		return vm.ToValue(base64.RawURLEncoding.EncodeToString([]byte(s)))
	}); err != nil {
		return err
	}
	if err := set("sha256", func(call goja.FunctionCall) goja.Value {
		s := utilityString(call, 0)
		sum := sha256.Sum256([]byte(s))
		return vm.ToValue(fmt.Sprintf("%x", sum))
	}); err != nil {
		return err
	}
	if err := set("hmacSHA256", func(call goja.FunctionCall) goja.Value {
		key := utilityString(call, 0)
		msg := utilityString(call, 1)
		mac := hmac.New(sha256.New, []byte(key))
		_, _ = mac.Write([]byte(msg))
		return vm.ToValue(fmt.Sprintf("%x", mac.Sum(nil)))
	}); err != nil {
		return err
	}
	if err := set("jwtSignHS256", func(call goja.FunctionCall) goja.Value {
		claims := call.Argument(0).Export()
		secret := utilityString(call, 1)
		header := `{"alg":"HS256","typ":"JWT"}`
		payload, err := json.Marshal(claims)
		if err != nil {
			panic(vm.ToValue("JWT claims 不能编码成 JSON"))
		}
		encodedHeader := base64.RawURLEncoding.EncodeToString([]byte(header))
		encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
		input := encodedHeader + "." + encodedPayload
		mac := hmac.New(sha256.New, []byte(secret))
		_, _ = mac.Write([]byte(input))
		return vm.ToValue(input + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)))
	}); err != nil {
		return err
	}
	if err := set("log", func(call goja.FunctionCall) goja.Value {
		if len(rt.logs) >= pluginproto.MaxLogsPerCall {
			return goja.Undefined()
		}
		msg := utilityString(call, 0)
		if len(msg) > 1024 {
			msg = msg[:1024]
		}
		rt.logs = append(rt.logs, msg)
		return goja.Undefined()
	}); err != nil {
		return err
	}
	return nil
}

func utilityString(call goja.FunctionCall, index int) string {
	value := call.Argument(index)
	s := value.String()
	if len([]byte(s)) > maxUtilityInput {
		panic("utils 输入超过 256KB")
	}
	return s
}

var errNoMeta = errors.New("插件没有导出 meta")

// encodeMeta 用 JS 自己的 JSON.stringify 序列化 meta。
// 不能用 Export() + json.Marshal：Go 的 map 会把键排成字母序，channelSettings / import.args 的书写顺序就丢了
// （管理端按此顺序渲染表单）。JSON.stringify 按属性定义顺序输出（整数样式的键除外，这是 JS 语言规则）。
// toJSON、getter 可能死循环，所以同样在超时保护内执行。
func encodeMeta(rt *runtime, timeout time.Duration) (json.RawMessage, error) {
	var out json.RawMessage
	err := runTimed(rt, timeout, func() error {
		metaValue := rt.exports.Get("meta")
		if goja.IsUndefined(metaValue) || goja.IsNull(metaValue) {
			return errNoMeta
		}
		stringify, ok := goja.AssertFunction(rt.vm.Get("JSON").ToObject(rt.vm).Get("stringify"))
		if !ok {
			return errors.New("JSON.stringify 不可用")
		}
		encoded, err := stringify(goja.Undefined(), metaValue)
		if err != nil {
			return err
		}
		if goja.IsUndefined(encoded) {
			return errInvalidResult
		}
		out = json.RawMessage(encoded.String())
		return nil
	})
	return out, err
}
