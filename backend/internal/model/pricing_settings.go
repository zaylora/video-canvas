package model

import (
	"encoding/json"
	"fmt"
)

// 定价设置键；实时汇率只由汇率服务更新。
const (
	SettingPricingCreditsPerYuan = "pricing_credits_per_yuan"
	SettingPricingMarkup         = "pricing_default_markup_pct"
	SettingPricingFXOverride     = "pricing_usd_cny_override"
	SettingPricingFXLast         = "pricing_usd_cny_last"
	SettingPricingFXDate         = "pricing_usd_cny_date"
	SettingPricingFXFetchedAt    = "pricing_usd_cny_fetched_at"
	AdminAuditSettingsPricing    = "settings.pricing"
	AuditModelUpdate             = "model.update"
)

// PricingSettingsReq 是人工定价配置，null 汇率表示使用实时值。
type PricingSettingsReq struct {
	CreditsPerYuan   float64  `json:"credits_per_yuan"`
	DefaultMarkupPct int      `json:"default_markup_pct"`
	USDCNYOverride   *float64 `json:"usd_cny_override"`
}

// UnmarshalJSON 区分必填的零加价率与字段缺失，汇率覆盖必须显式提供数字或 null。
func (r *PricingSettingsReq) UnmarshalJSON(raw []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	for _, key := range []string{"credits_per_yuan", "default_markup_pct", "usd_cny_override"} {
		value, ok := fields[key]
		if !ok || (string(value) == "null" && key != "usd_cny_override") {
			return fmt.Errorf("%s 为必填字段", key)
		}
	}
	type plain PricingSettingsReq
	return json.Unmarshal(raw, (*plain)(r))
}

// PricingSettingsView 是定价设置及当前生效汇率。
type PricingSettingsView struct {
	CreditsPerYuan   *float64      `json:"credits_per_yuan"`
	DefaultMarkupPct int           `json:"default_markup_pct"`
	USDCNYOverride   *float64      `json:"usd_cny_override"`
	USDCNY           PricingFXView `json:"usd_cny"`
}

// PricingFXView 描述汇率的来源和新鲜度。
type PricingFXView struct {
	Rate      *float64 `json:"rate"`
	Date      *string  `json:"date"`
	FetchedAt *string  `json:"fetched_at"`
	Source    string   `json:"source"`
	Stale     bool     `json:"stale"`
}
