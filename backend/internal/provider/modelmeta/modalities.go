package modelmeta

import (
	_ "embed"
	"encoding/json"
	"sync"
)

// modalitiesSnapshot 来自 models.dev 的 modalities.input，与上下文快照使用同一生成脚本。
//
//go:embed modalities.json
var modalitiesSnapshot []byte

// Modalities 是模型输入能力的建议；与渠道协议是否支持传输媒体是两个条件。
type Modalities struct {
	Vision bool // 支持图片输入
	Video  bool // 支持视频输入
}

var modalityData = sync.OnceValue(func() struct {
	exact    map[string][2]bool
	stripped map[string]string
} {
	data := struct {
		exact    map[string][2]bool
		stripped map[string]string
	}{exact: map[string][2]bool{}, stripped: map[string]string{}}
	// 内置快照损坏时不提供建议，保留运营手填的能力。
	if json.Unmarshal(modalitiesSnapshot, &data.exact) != nil {
		return data
	}
	for name := range data.exact {
		key := stripVersion(name)
		if name > data.stripped[key] {
			data.stripped[key] = name
		}
	}
	return data
})

// LookupModalities 按同一套名称规整规则识别图片和视频输入能力；未知模型返回 false，不猜测能力。
func LookupModalities(id string) (Modalities, bool) {
	data := modalityData()
	name := normalize(id)
	stripped := stripVersion(name)
	for _, key := range []string{name, stripped, data.stripped[stripped]} {
		if v, ok := data.exact[key]; ok {
			return Modalities{Vision: v[0], Video: v[1]}, true
		}
	}
	return Modalities{}, false
}
