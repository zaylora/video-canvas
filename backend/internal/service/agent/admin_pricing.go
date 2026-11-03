package agent

import (
	"fmt"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"video-canvas/internal/model"
)

// PricingBasis 是由平台复算后展示的调价依据。
type PricingBasis struct {
	Path           string           `json:"path"`
	Mode           string           `json:"mode"`
	Upstream       *PricingUpstream `json:"upstream,omitempty"`
	FX             *PricingFX       `json:"fx,omitempty"`
	CreditsPerYuan float64          `json:"credits_per_yuan,omitempty"`
	Yuan           float64          `json:"yuan,omitempty"`
	Cost           int              `json:"cost,omitempty"`
	MarkupPct      float64          `json:"markup_pct,omitempty"`
	From           float64          `json:"from,omitempty"`
	Pct            float64          `json:"pct,omitempty"`
	Price          int              `json:"price"`
}

// PricingUpstream 记录官方报价；单位换算由助手解释，平台只复算最终积分。
type PricingUpstream struct {
	Amount    float64 `json:"amount"`
	Currency  string  `json:"currency"`
	Unit      string  `json:"unit"`
	SourceURL string  `json:"source_url"`
	Note      string  `json:"note,omitempty"`
}

// PricingFX 是本次使用的服务端汇率快照。
type PricingFX struct {
	Rate   float64 `json:"rate"`
	Date   *string `json:"date"`
	Source string  `json:"source"`
	Stale  bool    `json:"stale"`
}

// AdminPriceCeil 先四舍五入到六位小数再向上取整，消除浮点整数边界的误差。
func AdminPriceCeil(n float64) int { return int(math.Ceil(math.Round(n*1e6) / 1e6)) }

// pricePath 只接受模型现有的售价字段；成本路径不能冒充售价依据。
var pricePath = regexp.MustCompile(`^pricing\.(unit|per_second|token\.(in|out)|tiers\[[0-9]+\]\.unit)$`)
var pricePointerPath = regexp.MustCompile(`^/pricing/(unit|per_second|token/(in|out)|tiers/(0|[1-9][0-9]*)/unit)$`)
var pathPart = regexp.MustCompile(`([^.[\]]+)|\[([0-9]+)\]`)

// normalizePricePath 将合法售价 JSON Pointer 转为点号与数组下标，非法路径留给原有校验拒绝。
func normalizePricePath(path string) string {
	if !pricePointerPath.MatchString(path) {
		return path
	}
	parts := strings.Split(path[1:], "/")
	if len(parts) == 4 {
		return "pricing.tiers[" + parts[2] + "].unit"
	}
	return strings.Join(parts, ".")
}

// valueAt 读取字段或数组路径；不存在返回 nil。
func valueAt(value any, path string) any {
	for _, part := range pathPart.FindAllStringSubmatch(path, -1) {
		if part[2] != "" {
			index, _ := strconv.Atoi(part[2])
			arr, ok := value.([]any)
			if !ok || index >= len(arr) {
				return nil
			}
			value = arr[index]
		} else {
			obj, ok := value.(map[string]any)
			if !ok {
				return nil
			}
			value = obj[part[1]]
		}
	}
	return value
}

// costPath 根据售价路径找到配置的成本字段，包括规格价独立的 cost。
func costPath(path, billing string) string {
	if strings.Contains(path, "tiers[") {
		field := "unit"
		if billing == "per_second" {
			field = "per_second"
		}
		return strings.TrimSuffix(path, "unit") + "cost." + field
	}
	return strings.Replace(path, "pricing.", "pricing.cost.", 1)
}

// recomputeBasis 使用服务端汇率和积分比例，不信任模型给出的中间数值。
func recomputeBasis(b PricingBasis, settings *model.PricingSettingsView, before any) (PricingBasis, error) {
	if !pricePath.MatchString(b.Path) {
		return b, fmt.Errorf("价格路径 %s 不合法", b.Path)
	}
	switch b.Mode {
	case "relative":
		from, ok := valueAt(before, b.Path).(float64)
		if !ok || from < 0 || from != b.From || !finite(b.Pct) || b.Pct < -100 {
			return b, fmt.Errorf("%s 的原价或调价比例不合法", b.Path)
		}
		b.Price = AdminPriceCeil(from * (1 + b.Pct/100))
	case "cost":
		var err error
		b, err = recomputeCostBasis(b, settings)
		if err != nil {
			return b, err
		}
	default:
		return b, fmt.Errorf("定价依据 mode 只能是 cost 或 relative")
	}
	if b.Price < 0 || b.Price > math.MaxInt32 {
		return b, fmt.Errorf("%s 的价格超出范围", b.Path)
	}
	return b, nil
}

// recomputeCostBasis 验证官方报价与服务端换算设置，再分别取整成本与售价。
func recomputeCostBasis(b PricingBasis, settings *model.PricingSettingsView) (PricingBasis, error) {
	u := b.Upstream
	if u == nil || !finite(u.Amount) || u.Amount < 0 || u.Unit == "" || u.SourceURL == "" || (u.Currency != "USD" && u.Currency != "CNY") {
		return b, fmt.Errorf("%s 缺少合法的官方报价、单位或 source_url", b.Path)
	}
	source, err := url.Parse(u.SourceURL)
	if err != nil || source.Host == "" || (source.Scheme != "https" && source.Scheme != "http") || source.User != nil {
		return b, fmt.Errorf("%s 的 source_url 必须是合法的官方网页地址", b.Path)
	}
	if settings.CreditsPerYuan == nil {
		return b, fmt.Errorf("请先设置 credits_per_yuan（1 元对应的积分）")
	}
	rate := 1.0
	b.FX = nil
	if u.Currency == "USD" {
		fx := settings.USDCNY
		if fx.Rate == nil {
			return b, fmt.Errorf("从未取得美元汇率，请先设置汇率覆盖或等待拉取成功")
		}
		rate = *fx.Rate
		b.FX = &PricingFX{Rate: rate, Date: fx.Date, Source: fx.Source, Stale: fx.Stale}
	}
	if !finite(b.MarkupPct) || b.MarkupPct < 0 || b.MarkupPct > 1000 {
		return b, fmt.Errorf("markup_pct 必须在 0–1000 之间")
	}
	yuan := u.Amount * rate
	if !finite(yuan) || !finite(yuan**settings.CreditsPerYuan) {
		return b, fmt.Errorf("%s 的成本超出范围", b.Path)
	}
	b.Yuan = math.Round(yuan*1e4) / 1e4
	b.CreditsPerYuan = *settings.CreditsPerYuan
	b.Cost = AdminPriceCeil(yuan * b.CreditsPerYuan)
	b.Price = AdminPriceCeil(float64(b.Cost) * (1 + b.MarkupPct/100))
	return b, nil
}

// finite 防止极值或非法浮点进入取整与配置。
func finite(n float64) bool {
	return !math.IsNaN(n) && !math.IsInf(n, 0) && math.Abs(n) < float64(math.MaxInt32)
}
