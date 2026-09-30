package pluginrunner

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dop251/goja"
	"github.com/google/uuid"

	"video-canvas/internal/provider/pluginproto"
)

const bootstrap = `
var module = {exports: {}};
var exports = module.exports;
var utils = {};
`

// 动态代码入口并不是完整的安全边界；内存和网络的硬隔离依赖 runner 容器。
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
  try { Object.defineProperty(Object.getPrototypeOf(function*(){}), "constructor", {value: undefined}); } catch (_) {}
  for (var j = 0; j < constructors.length; j++) if (constructors[j]) Object.freeze(constructors[j]);
  Object.freeze(JSON); Object.freeze(Math); Object.freeze(Reflect);
  Object.freeze(utils);
  globalThis.eval = undefined;
  globalThis.Function = undefined;
})();
`

func compile(code string) (*goja.Program, error) {
	return goja.Compile("plugin.js", bootstrap+"\n"+code, false)
}

func newRuntime(program *goja.Program) (*runtime, error) {
	vm := goja.New()
	rt := &runtime{vm: vm}
	if _, err := vm.RunProgram(program); err != nil {
		return nil, err
	}
	if err := installUtils(rt); err != nil {
		return nil, err
	}
	lockdownProgram, err := goja.Compile("lockdown.js", lockdown, false)
	if err != nil {
		return nil, err
	}
	if _, err := vm.RunProgram(lockdownProgram); err != nil {
		return nil, err
	}
	exports := vm.Get("module").ToObject(vm).Get("exports")
	if goja.IsUndefined(exports) || goja.IsNull(exports) {
		return nil, errors.New("module.exports 必须是对象")
	}
	rt.exports = exports.ToObject(vm)
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
		rt.vm.Interrupt("钩子执行超时")
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

var errTimeout = errors.New("钩子执行超时")

func invoke(rt *runtime, name string, args []json.RawMessage, timeout time.Duration) (json.RawMessage, error) {
	fn, ok := goja.AssertFunction(rt.exports.Get(name))
	if !ok {
		return nil, errMissing
	}
	rt.logs = nil
	var result json.RawMessage
	err := runTimed(rt, timeout, func() error {
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

// 保留 crypto/rand 的导入，避免未来替换 UUID 实现时重新改变随机源边界。
var _ = rand.Reader
var _ = strings.TrimSpace
