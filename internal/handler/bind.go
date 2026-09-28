package handler

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/locales/zh"
	ut "github.com/go-playground/universal-translator"
	"github.com/go-playground/validator/v10"
	zhtrans "github.com/go-playground/validator/v10/translations/zh"

	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/response"
)

// trans 把 validator 的校验错误翻译成中文。
var trans ut.Translator

func init() {
	v, ok := binding.Validator.Engine().(*validator.Validate)
	if !ok {
		return
	}
	// 错误提示中的字段名：优先取 label 标签（中文名），其次取 json 标签。
	v.RegisterTagNameFunc(func(f reflect.StructField) string {
		if label := f.Tag.Get("label"); label != "" {
			return label
		}
		name := strings.SplitN(f.Tag.Get("json"), ",", 2)[0]
		if name == "-" {
			return ""
		}
		return name
	})
	locale := zh.New()
	trans, _ = ut.New(locale, locale).GetTranslator("zh")
	_ = zhtrans.RegisterDefaultTranslations(v, trans)
}

// bindJSON 解析 JSON 请求体到 req，失败时直接返回中文错误提示并返回 false。
func bindJSON(c *gin.Context, req any) bool {
	return bindWith(c, c.ShouldBindJSON(req))
}

// bindQuery 解析 URL 查询参数到 req，行为同 bindJSON。
func bindQuery(c *gin.Context, req any) bool {
	return bindWith(c, c.ShouldBindQuery(req))
}

func bindWith(c *gin.Context, err error) bool {
	if err == nil {
		return true
	}
	response.Fail(c, errcode.ErrInvalidParams.WithMsg(bindErrMsg(err)))
	return false
}

// bindErrMsg 把绑定/校验错误转成面向用户的中文提示。
func bindErrMsg(err error) string {
	var ve validator.ValidationErrors
	if errors.As(err, &ve) && trans != nil {
		msgs := make([]string, 0, len(ve))
		for _, fe := range ve {
			msgs = append(msgs, fe.Translate(trans))
		}
		return strings.Join(msgs, "；")
	}
	var te *json.UnmarshalTypeError
	if errors.As(err, &te) {
		return te.Field + " 类型错误"
	}
	return "请求参数格式错误"
}
