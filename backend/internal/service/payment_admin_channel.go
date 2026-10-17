package service

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"strings"
	"time"

	"gorm.io/datatypes"

	"video-canvas/internal/model"
	"video-canvas/internal/payment"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
)

const (
	defaultChannelTimeout = 15               // 新渠道的未支付订单自动关闭时间（分钟）
	maxFieldLen           = 4096             // 单个配置字段的最大字节数：正常的密钥、地址远小于它，超出多半是粘贴错了内容
	probeTimeout          = 20 * time.Second // 校验时在线探测的超时
)

// requireHTTPSNotify 列出要求回调地址必须是 https 的渠道：微信支付拒绝 http 回调地址。
var requireHTTPSNotify = map[string]bool{model.ProviderWechatNative: true}

// channelState 是构造渠道视图需要的上下文：当前设置和所有渠道行，列表时只读一次。
type channelState struct {
	st   *paymentSettings
	rows map[string]*model.PaymentChannel
}

// loadChannelState 读取设置和全部渠道行。
func (s *PaymentAdminService) loadChannelState(ctx context.Context) (*channelState, error) {
	kv, err := s.settings.GetAll(ctx)
	if err != nil {
		return nil, err
	}
	list, err := s.channelsAdmin.ListChannels(ctx)
	if err != nil {
		return nil, err
	}
	cs := &channelState{st: parsePaymentSettings(kv), rows: make(map[string]*model.PaymentChannel, len(list))}
	for i := range list {
		cs.rows[list[i].Provider] = &list[i]
	}
	return cs, nil
}

// ListChannels 返回全部内置渠道（包括还没配置过的）：字段定义、当前值、状态、能力和回调地址。敏感字段只返回是否已设置。
func (s *PaymentAdminService) ListChannels(ctx context.Context) ([]model.PaymentChannelView, error) {
	cs, err := s.loadChannelState(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]model.PaymentChannelView, 0, len(s.registry.Keys()))
	for _, key := range s.registry.Keys() {
		v, err := s.channelView(ctx, cs, key)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, nil
}

// channelView 构造一个渠道的视图。
func (s *PaymentAdminService) channelView(ctx context.Context, cs *channelState, key string) (*model.PaymentChannelView, error) {
	p, _ := s.registry.Get(key)
	caps := p.Capabilities()
	row := cs.rows[key]
	v := &model.PaymentChannelView{
		Provider: key, Name: model.PaymentProviderNames[key], Methods: caps.Methods, Checkout: caps.Checkout,
		CanQuery: caps.CanQuery, CanCloseUpstream: caps.CanCloseUpstream, TimeoutMinutes: defaultChannelTimeout, Serving: []string{},
	}
	var cfg map[string]string
	var set map[string]bool
	if row != nil {
		v.Configured, v.Enabled, v.Validated, v.ValidatedAt = true, row.Enabled, row.ValidatedAt != nil, row.ValidatedAt
		v.Version, v.TimeoutMinutes, v.UpdatedAt = row.Version, row.OrderTimeoutMinutes, &row.UpdatedAt
		_ = json.Unmarshal(row.ConfigJSON, &cfg) // 损坏的配置按空处理：页面上重新填写即可
		_ = json.Unmarshal(row.SecretSet, &set)
	}
	for _, f := range p.Fields() {
		fv := model.PaymentFieldView{Key: f.Key, Label: f.Label, Secret: f.Secret, Identity: f.Identity, Optional: f.Optional, Default: f.Default, Placeholder: f.Placeholder}
		if f.Secret {
			fv.SecretSet = set[f.Key]
		} else {
			fv.Value = cfg[f.Key]
		}
		v.Fields = append(v.Fields, fv)
	}
	n, err := s.channelsAdmin.CountOpenOrdersByProvider(ctx, key)
	if err != nil {
		return nil, err
	}
	v.OpenOrders = n
	for _, m := range model.PayMethods {
		if cs.st.Methods[m] == key {
			v.Serving = append(v.Serving, m)
		}
	}
	if cs.st.BaseURL != "" {
		v.NotifyURL = cs.st.BaseURL + "/api/v1/payment/notify/" + key
	}
	return v, nil
}

// viewOf 读取最新状态后构造单个渠道的视图。
func (s *PaymentAdminService) viewOf(ctx context.Context, key string) (*model.PaymentChannelView, error) {
	cs, err := s.loadChannelState(ctx)
	if err != nil {
		return nil, err
	}
	return s.channelView(ctx, cs, key)
}

// provider 取适配器，渠道标识未知返回 404。
func (s *PaymentAdminService) provider(key string) (payment.Provider, error) {
	p, ok := s.registry.Get(key)
	if !ok {
		return nil, errcode.ErrNotFound.WithMsg("支付渠道不存在")
	}
	return p, nil
}

// channelRow 读取渠道行；不存在返回 (nil, nil)。
func (s *PaymentAdminService) channelRow(ctx context.Context, key string) (*model.PaymentChannel, error) {
	row, err := s.channelsAdmin.GetChannel(ctx, key)
	if isNotFound(err) {
		return nil, nil
	}
	return row, err
}

// channelEdit 是一次保存要写入的变化：新的非敏感配置、要覆盖的密钥，以及它们相对旧值改了什么。
type channelEdit struct {
	config          map[string]string // 非敏感字段的完整新值
	secrets         map[string]string // 要覆盖的敏感字段（已去掉留空的）
	changedKeys     []string          // 变了的非敏感字段
	identityChanged bool              // 变了的字段里有商户身份字段
}

// planEdit 校验请求里的字段并算出变化。字段必须是渠道定义里的；密钥只能走 secrets、普通字段只能走 config；单个值不超过 maxFieldLen。
func planEdit(p payment.Provider, old map[string]string, req *model.UpdatePaymentChannelReq) (*channelEdit, error) {
	defs := make(map[string]payment.Field)
	for _, f := range p.Fields() {
		defs[f.Key] = f
	}
	if err := checkFieldKeys(defs, req); err != nil {
		return nil, err
	}
	e := &channelEdit{config: map[string]string{}, secrets: map[string]string{}}
	for k, v := range req.Secrets {
		if strings.TrimSpace(v) != "" { // 留空表示保持原值
			e.secrets[k] = strings.TrimSpace(v)
		}
	}
	for _, f := range p.Fields() {
		if f.Secret {
			continue
		}
		nv := strings.TrimSpace(req.Config[f.Key])
		if nv != "" {
			e.config[f.Key] = nv
		}
		if nv != old[f.Key] {
			e.changedKeys = append(e.changedKeys, f.Key)
			e.identityChanged = e.identityChanged || f.Identity
		}
	}
	return e, nil
}

// checkFieldKeys 校验请求里的字段名和值：config 里只能是渠道定义的非敏感字段，secrets 里只能是敏感字段，值不能过长。
func checkFieldKeys(defs map[string]payment.Field, req *model.UpdatePaymentChannelReq) error {
	for k, v := range req.Config {
		if f, ok := defs[k]; !ok || f.Secret {
			return errcode.ErrInvalidParams.WithMsg("未知的配置字段：" + k)
		}
		if len(v) > maxFieldLen {
			return errcode.ErrInvalidParams.WithMsg("字段值过长：" + k)
		}
	}
	for k, v := range req.Secrets {
		if f, ok := defs[k]; !ok || !f.Secret {
			return errcode.ErrInvalidParams.WithMsg("未知的密钥字段：" + k)
		}
		if len(v) > maxFieldLen {
			return errcode.ErrInvalidParams.WithMsg("字段值过长：" + k)
		}
	}
	return nil
}

// UpdateChannel 保存渠道配置（仅 super_admin，由路由保证）。规则：
//   - 乐观锁：Version 必须等于当前版本（首次保存为 0），否则 65011；
//   - 渠道有未完成订单时，不能改商户身份字段、不能换密钥（65010）：回调要用下单时的商户身份和密钥验签；
//   - 敏感字段每个单独加密保存，留空表示保持原值，接口永远不返回它们；
//   - 改了配置或密钥后，已启用的渠道自动停用、校验结果作废，要重新校验再启用；只改超时时间不受影响。
func (s *PaymentAdminService) UpdateChannel(ctx context.Context, actorID uint64, key string, req *model.UpdatePaymentChannelReq) (*model.PaymentChannelView, error) {
	p, err := s.provider(key)
	if err != nil {
		return nil, err
	}
	row, err := s.channelRow(ctx, key)
	if err != nil {
		return nil, err
	}
	cur := 0
	if row != nil {
		cur = row.Version
	}
	if req.Version != cur {
		return nil, errcode.ErrPayVersionConflict
	}
	var oldCfg map[string]string
	if row != nil {
		_ = json.Unmarshal(row.ConfigJSON, &oldCfg)
	}
	edit, err := planEdit(p, oldCfg, req)
	if err != nil {
		return nil, err
	}
	if err := s.checkLocked(ctx, key, edit); err != nil {
		return nil, err
	}
	// 密钥先写：写不了（比如没配置主密钥）就整次保存失败，不会留下「标记已设置、实际没有」的渠道行
	if err := s.writeSecrets(ctx, actorID, key, edit.secrets); err != nil {
		return nil, err
	}
	if err := s.saveRow(ctx, actorID, key, row, edit, req); err != nil {
		return nil, err
	}
	adminAudit(ctx, s.audit, actorID, model.AdminAuditPayChannelUpdate, model.AdminAuditTargetPayment, 0, map[string]any{
		"provider": key, "changed_fields": edit.changedKeys, "secrets_replaced": sortedKeys(edit.secrets),
	})
	return s.viewOf(ctx, key)
}

// checkLocked 渠道有未完成订单时，拒绝改商户身份字段和换密钥。
func (s *PaymentAdminService) checkLocked(ctx context.Context, key string, e *channelEdit) error {
	if !e.identityChanged && len(e.secrets) == 0 {
		return nil
	}
	n, err := s.channelsAdmin.CountOpenOrdersByProvider(ctx, key)
	if err != nil {
		return err
	}
	if n > 0 {
		return errcode.ErrPayChannelLocked
	}
	return nil
}

// writeSecrets 把传入的敏感字段逐个加密保存。
func (s *PaymentAdminService) writeSecrets(ctx context.Context, actorID uint64, key string, secrets map[string]string) error {
	for _, field := range sortedKeys(secrets) {
		if err := s.secrets.SetSecret(ctx, paymentSecretName(key, field), secrets[field], actorID); err != nil {
			return err
		}
	}
	return nil
}

// saveRow 写渠道行：首次保存创建，之后乐观锁更新。
func (s *PaymentAdminService) saveRow(ctx context.Context, actorID uint64, key string, row *model.PaymentChannel, e *channelEdit, req *model.UpdatePaymentChannelReq) error {
	cfgJSON, err := json.Marshal(e.config)
	if err != nil {
		return err
	}
	set := map[string]bool{}
	if row != nil {
		_ = json.Unmarshal(row.SecretSet, &set)
	}
	for k := range e.secrets {
		set[k] = true
	}
	setJSON, err := json.Marshal(set)
	if err != nil {
		return err
	}
	timeout := defaultChannelTimeout
	if row != nil {
		timeout = row.OrderTimeoutMinutes
	}
	if req.TimeoutMinutes != nil {
		timeout = *req.TimeoutMinutes
	}
	if row == nil {
		err = s.channelsAdmin.CreateChannel(ctx, &model.PaymentChannel{
			Provider: key, ConfigJSON: cfgJSON, SecretSet: setJSON, OrderTimeoutMinutes: timeout, Version: 1, UpdatedBy: actorID,
		})
		return mapChannelConflict(err)
	}
	fields := map[string]any{"config_json": datatypes.JSON(cfgJSON), "secret_set": datatypes.JSON(setJSON), "order_timeout_minutes": timeout, "updated_by": actorID}
	if len(e.changedKeys) > 0 || len(e.secrets) > 0 { // 配置变了：校验作废，已启用的自动停用
		fields["enabled"], fields["validated_at"] = false, nil
	}
	return mapChannelConflict(s.channelsAdmin.UpdateChannelIf(ctx, key, row.Version, fields))
}

// mapChannelConflict 把仓储的乐观锁冲突翻译成业务错误。
func mapChannelConflict(err error) error {
	if errors.Is(err, repository.ErrRevisionConflict) {
		return errcode.ErrPayVersionConflict
	}
	return err
}

// ValidateChannel 校验渠道配置：先本地格式校验，再在线探测凭证（用一个不存在的订单号查单）。
// 通过后记录校验时间，渠道才能启用；校验期间配置又被改过（版本变了）则不记，返回 65011。
func (s *PaymentAdminService) ValidateChannel(ctx context.Context, _ uint64, key string) (*model.PaymentChannelView, error) {
	if _, err := s.provider(key); err != nil {
		return nil, err
	}
	lp, err := s.loadProvider(ctx, key)
	if isNotFound(err) {
		return nil, errcode.ErrPayChannelConfig.WithMsg("渠道还没有保存过配置")
	}
	if err != nil {
		return nil, err
	}
	if err := lp.provider.ValidateConfig(lp.cfg); err != nil {
		return nil, errcode.ErrPayChannelConfig.WithMsg("配置不完整或格式不对：" + failReason(err))
	}
	pctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	skipped := false
	if err := lp.provider.Probe(pctx, lp.cfg); errors.Is(err, payment.ErrProbeUnsupported) {
		skipped = true // 无法在线探测的渠道只做了本地校验，页面提示用小额真实订单测试
	} else if err != nil {
		return nil, errcode.ErrPayChannelConfig.WithMsg("在线校验未通过：" + failReason(err))
	}
	if err := s.channelsAdmin.SetChannelValidated(ctx, key, lp.channel.Version, s.now()); err != nil {
		return nil, mapChannelConflict(err)
	}
	v, err := s.viewOf(ctx, key)
	if err == nil {
		v.ProbeSkipped = skipped
	}
	return v, err
}

// EnableChannel 启用渠道：要求当前配置已通过校验、已配置站点公网地址（微信还要求是 https）。写审计。
func (s *PaymentAdminService) EnableChannel(ctx context.Context, actorID uint64, key string) (*model.PaymentChannelView, error) {
	if _, err := s.provider(key); err != nil {
		return nil, err
	}
	row, err := s.channelRow(ctx, key)
	if err != nil {
		return nil, err
	}
	if row == nil || row.ValidatedAt == nil {
		return nil, errcode.ErrPayChannelConfig.WithMsg("请先保存配置并通过校验")
	}
	kv, err := s.settings.GetAll(ctx)
	if err != nil {
		return nil, err
	}
	base := parsePaymentSettings(kv).BaseURL
	if base == "" {
		return nil, errcode.ErrPayBaseURLMissing
	}
	if requireHTTPSNotify[key] && !strings.HasPrefix(base, "https://") {
		return nil, errcode.ErrPayChannelConfig.WithMsg(model.PaymentProviderNames[key] + " 要求回调地址是 https，请把站点公网地址改成 https 开头")
	}
	if err := s.channelsAdmin.SetChannelEnabled(ctx, key, true); err != nil {
		return nil, err
	}
	adminAudit(ctx, s.audit, actorID, model.AdminAuditPayChannelEnable, model.AdminAuditTargetPayment, 0, map[string]any{"provider": key})
	return s.viewOf(ctx, key)
}

// DisableChannel 停用渠道。它承接的支付方式立刻在前台不可用；已有订单的回调、查单、补单照常工作。写审计。
// 返回的视图里 Serving 是受影响的支付方式，前端据此提示。
func (s *PaymentAdminService) DisableChannel(ctx context.Context, actorID uint64, key string) (*model.PaymentChannelView, error) {
	if _, err := s.provider(key); err != nil {
		return nil, err
	}
	err := s.channelsAdmin.SetChannelEnabled(ctx, key, false)
	if isNotFound(err) {
		return nil, errcode.ErrPayChannelConfig.WithMsg("渠道还没有保存过配置")
	}
	if err != nil {
		return nil, err
	}
	adminAudit(ctx, s.audit, actorID, model.AdminAuditPayChannelDisable, model.AdminAuditTargetPayment, 0, map[string]any{"provider": key})
	return s.viewOf(ctx, key)
}

// sortedKeys 返回 map 的键，升序；审计和写密钥都要稳定的顺序。
func sortedKeys[V any](m map[string]V) []string { return slices.Sorted(maps.Keys(m)) }
