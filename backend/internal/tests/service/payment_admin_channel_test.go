package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
)

func TestPaymentAdmin_Settings(t *testing.T) {
	ctx := context.Background()

	t.Run("读取：返回当前设置，未设置的上限取缺省 3", func(t *testing.T) {
		e := newAdminEnv(t)
		v, err := e.admin.Settings(ctx)
		if err != nil || !v.Enabled || v.PublicBaseURL != payTestBase || v.MethodWechat != model.ProviderWechatNative || v.MaxPending != 3 {
			t.Fatalf("%+v %v", v, err)
		}
	})
	t.Run("保存：写库、去掉公网地址末尾斜杠、写审计", func(t *testing.T) {
		e := newAdminEnv(t)
		v, err := e.admin.UpdateSettings(ctx, 9, &model.UpdatePaymentSettingsReq{
			Enabled: true, PublicBaseURL: " https://new.example.com/ ", MethodWechat: model.ProviderWechatNative, MethodAlipay: "", MaxPending: 5,
		})
		if err != nil || v.PublicBaseURL != "https://new.example.com" || v.MaxPending != 5 || v.MethodAlipay != "" {
			t.Fatalf("%+v %v", v, err)
		}
		if e.kv.kv[model.SettingPaymentPublicBaseURL] != "https://new.example.com" || e.kv.kv[model.SettingPaymentMaxPending] != "5" || e.kv.kv[model.SettingPaymentMethodAlipay] != "" {
			t.Fatalf("库值不对：%v", e.kv.kv)
		}
		if acts := e.audit.actions(); len(acts) != 1 || acts[0] != model.AdminAuditPaySettings {
			t.Fatalf("审计不对：%v", acts)
		}
	})
	base := model.UpdatePaymentSettingsReq{Enabled: true, PublicBaseURL: payTestBase, MethodWechat: model.ProviderWechatNative, MethodAlipay: model.ProviderAlipayPage, MaxPending: 3}
	tests := []struct {
		name     string
		mutate   func(r *model.UpdatePaymentSettingsReq, e *adminEnv)
		wantCode int
	}{
		{"开启支付但没配公网地址返回 65013", func(r *model.UpdatePaymentSettingsReq, _ *adminEnv) { r.PublicBaseURL = "" }, errcode.ErrPayBaseURLMissing.Code},
		{"公网地址带路径返回 10001", func(r *model.UpdatePaymentSettingsReq, _ *adminEnv) { r.PublicBaseURL = "https://a.com/pay" }, errcode.ErrInvalidParams.Code},
		{"公网地址不是 http(s) 返回 10001", func(r *model.UpdatePaymentSettingsReq, _ *adminEnv) { r.PublicBaseURL = "ftp://a.com" }, errcode.ErrInvalidParams.Code},
		{"承接渠道不存在返回 10001", func(r *model.UpdatePaymentSettingsReq, _ *adminEnv) { r.MethodWechat = "nope" }, errcode.ErrInvalidParams.Code},
		{"承接渠道不支持该支付方式返回 10001", func(r *model.UpdatePaymentSettingsReq, _ *adminEnv) { r.MethodAlipay = model.ProviderWechatNative }, errcode.ErrInvalidParams.Code},
		{"承接渠道没启用返回 65009", func(r *model.UpdatePaymentSettingsReq, e *adminEnv) {
			e.store.channels[model.ProviderWechatNative].Enabled = false
		}, errcode.ErrPayChannelConfig.Code},
		{"未支付上限越界返回 10001", func(r *model.UpdatePaymentSettingsReq, _ *adminEnv) { r.MaxPending = 11 }, errcode.ErrInvalidParams.Code},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newAdminEnv(t)
			req := base
			tt.mutate(&req, e)
			_, err := e.admin.UpdateSettings(ctx, 9, &req)
			wantPayCode(t, err, tt.wantCode)
			if len(e.audit.actions()) != 0 {
				t.Fatal("失败不能写审计")
			}
		})
	}
	t.Run("关闭支付时不要求公网地址", func(t *testing.T) {
		e := newAdminEnv(t)
		if _, err := e.admin.UpdateSettings(ctx, 9, &model.UpdatePaymentSettingsReq{Enabled: false, MaxPending: 3}); err != nil {
			t.Fatal(err)
		}
	})
}

func TestPaymentAdmin_ListChannels(t *testing.T) {
	ctx := context.Background()
	e := newAdminEnv(t)
	e.store.orders[900] = openOrder(900, model.ProviderWechatNative)
	list, err := e.admin.ListChannels(ctx)
	if err != nil || len(list) != 2 {
		t.Fatalf("应列出注册表里的全部渠道：%d %v", len(list), err)
	}
	var wx *model.PaymentChannelView
	for i := range list {
		if list[i].Provider == model.ProviderWechatNative {
			wx = &list[i]
		}
	}
	if wx == nil || !wx.Configured || !wx.Enabled || !wx.Validated || wx.Version != 3 || wx.OpenOrders != 1 || wx.TimeoutMinutes != 15 {
		t.Fatalf("%+v", wx)
	}
	if wx.NotifyURL != payTestBase+"/api/v1/payment/notify/wechat_native" || len(wx.Serving) != 1 || wx.Serving[0] != model.PayMethodWechat {
		t.Fatalf("回调地址或承接方式不对：%+v", wx)
	}
	for _, f := range wx.Fields {
		switch f.Key {
		case "app_id":
			if f.Value != "wx-app" || !f.Identity {
				t.Fatalf("非敏感字段应返回当前值：%+v", f)
			}
		case "api_key":
			if f.Value != "" || !f.SecretSet || !f.Secret {
				t.Fatalf("敏感字段只能说明是否已设置，不能返回值：%+v", f)
			}
		case "extra_secret":
			if f.SecretSet {
				t.Fatalf("没设置过的密钥应显示未设置：%+v", f)
			}
		}
	}
	b, _ := json.Marshal(list)
	if strings.Contains(string(b), `"k"`) {
		t.Fatalf("响应里不能出现密钥明文：%s", b)
	}
}

func openOrder(id uint64, provider string) *model.PaymentOrder {
	return &model.PaymentOrder{ID: id, OrderNo: "VC-OPEN", UserID: 1, Provider: provider, Status: model.PayOrderPending, ProductID: 1}
}

func TestPaymentAdmin_UpdateChannel(t *testing.T) {
	ctx := context.Background()
	const prov = model.ProviderWechatNative

	t.Run("首次保存（版本 0）：创建渠道行，密钥加密保存，未启用也未校验，审计只记字段名不记值", func(t *testing.T) {
		e := newAdminEnv(t)
		delete(e.store.channels, prov)
		delete(e.secret.kv, "payment:"+prov+":api_key")
		v, err := e.admin.UpdateChannel(ctx, 9, prov, &model.UpdatePaymentChannelReq{
			Version: 0, Config: map[string]string{"app_id": "wx-new", "gateway": " https://g.example.com "}, Secrets: map[string]string{"api_key": "TOP-SECRET-1"},
		})
		if err != nil || v.Enabled || v.Validated || v.Version != 1 || v.TimeoutMinutes != 15 || !v.Configured {
			t.Fatalf("%+v %v", v, err)
		}
		row := e.store.channels[prov]
		var cfg map[string]string
		_ = json.Unmarshal(row.ConfigJSON, &cfg)
		if cfg["app_id"] != "wx-new" || cfg["gateway"] != "https://g.example.com" || strings.Contains(string(row.ConfigJSON), "TOP-SECRET") {
			t.Fatalf("配置里只能有非敏感字段：%s", row.ConfigJSON)
		}
		if got := e.secret.kv["payment:"+prov+":api_key"]; got != "TOP-SECRET-1" {
			t.Fatalf("密钥应按字段单独存进密钥存储：%q", got)
		}
		if !strings.Contains(string(row.SecretSet), `"api_key":true`) {
			t.Fatalf("应记录 api_key 已设置：%s", row.SecretSet)
		}
		a := e.audit.logs
		if len(a) != 1 || a[0].Action != model.AdminAuditPayChannelUpdate || strings.Contains(string(a[0].DetailJSON), "TOP-SECRET") || !strings.Contains(string(a[0].DetailJSON), "api_key") {
			t.Fatalf("审计只记字段名：%+v", a)
		}
	})
	t.Run("版本不对返回 65011；首次保存带了非 0 版本也是冲突", func(t *testing.T) {
		e := newAdminEnv(t)
		_, err := e.admin.UpdateChannel(ctx, 9, prov, &model.UpdatePaymentChannelReq{Version: 2, Config: map[string]string{"app_id": "x"}})
		wantPayCode(t, err, errcode.ErrPayVersionConflict.Code)
		delete(e.store.channels, prov)
		_, err = e.admin.UpdateChannel(ctx, 9, prov, &model.UpdatePaymentChannelReq{Version: 5, Config: map[string]string{"app_id": "x"}})
		wantPayCode(t, err, errcode.ErrPayVersionConflict.Code)
	})
	t.Run("未知渠道返回 404", func(t *testing.T) {
		e := newAdminEnv(t)
		_, err := e.admin.UpdateChannel(ctx, 9, "nope", &model.UpdatePaymentChannelReq{})
		wantPayCode(t, err, errcode.ErrNotFound.Code)
	})
	badFields := map[string]model.UpdatePaymentChannelReq{
		"未知的配置字段":         {Version: 3, Config: map[string]string{"nope": "x"}},
		"把密钥字段放进 config":  {Version: 3, Config: map[string]string{"api_key": "x"}},
		"把普通字段放进 secrets": {Version: 3, Secrets: map[string]string{"app_id": "x"}},
		"字段值过长":           {Version: 3, Config: map[string]string{"app_id": strings.Repeat("x", 5000)}},
	}
	for name, req := range badFields {
		t.Run(name+"返回 10001", func(t *testing.T) {
			e := newAdminEnv(t)
			req := req
			_, err := e.admin.UpdateChannel(ctx, 9, prov, &req)
			wantPayCode(t, err, errcode.ErrInvalidParams.Code)
		})
	}
	t.Run("密钥留空表示保持原值，只覆盖传了的", func(t *testing.T) {
		e := newAdminEnv(t)
		e.secret.kv["payment:"+prov+":api_key"] = "OLD-KEY"
		e.secret.kv["payment:"+prov+":extra_secret"] = "OLD-EXTRA"
		_, err := e.admin.UpdateChannel(ctx, 9, prov, &model.UpdatePaymentChannelReq{
			Version: 3, Config: map[string]string{"app_id": "wx-app"}, Secrets: map[string]string{"api_key": "", "extra_secret": "NEW-EXTRA"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if e.secret.kv["payment:"+prov+":api_key"] != "OLD-KEY" || e.secret.kv["payment:"+prov+":extra_secret"] != "NEW-EXTRA" {
			t.Fatalf("留空的密钥保持原值、传了的覆盖：%v", e.secret.kv)
		}
	})
	t.Run("有未完成订单时：改商户身份字段或换密钥返回 65010，其它字段仍可改", func(t *testing.T) {
		for name, req := range map[string]model.UpdatePaymentChannelReq{
			"改身份字段": {Version: 3, Config: map[string]string{"app_id": "wx-changed"}},
			"换密钥":   {Version: 3, Config: map[string]string{"app_id": "wx-app"}, Secrets: map[string]string{"api_key": "NEW"}},
		} {
			e := newAdminEnv(t)
			e.store.orders[900] = openOrder(900, prov)
			req := req
			_, err := e.admin.UpdateChannel(ctx, 9, prov, &req)
			wantPayCode(t, err, errcode.ErrPayChannelLocked.Code)
			if e.store.channels[prov].Version != 3 {
				t.Fatalf("%s：被拒绝的保存不能改数据", name)
			}
		}
		e := newAdminEnv(t)
		e.store.orders[900] = openOrder(900, prov)
		_, err := e.admin.UpdateChannel(ctx, 9, prov, &model.UpdatePaymentChannelReq{Version: 3, Config: map[string]string{"app_id": "wx-app", "gateway": "https://other.example.com"}})
		if err != nil {
			t.Fatalf("改非身份字段应允许：%v", err)
		}
	})
	t.Run("改了配置：已启用的渠道自动停用并清空校验；只改超时时间则保持启用和已校验", func(t *testing.T) {
		e := newAdminEnv(t)
		timeout := 30
		v, err := e.admin.UpdateChannel(ctx, 9, prov, &model.UpdatePaymentChannelReq{Version: 3, Config: map[string]string{"app_id": "wx-app"}, TimeoutMinutes: &timeout})
		if err != nil || !v.Enabled || !v.Validated || v.TimeoutMinutes != 30 || v.Version != 4 {
			t.Fatalf("只改超时应保持启用和已校验：%+v %v", v, err)
		}
		v, err = e.admin.UpdateChannel(ctx, 9, prov, &model.UpdatePaymentChannelReq{Version: 4, Config: map[string]string{"app_id": "wx-app", "gateway": "https://changed.example.com"}})
		if err != nil || v.Enabled || v.Validated || v.Version != 5 {
			t.Fatalf("改了配置应自动停用并清空校验：%+v %v", v, err)
		}
	})
}

func TestPaymentAdmin_ValidateEnableDisable(t *testing.T) {
	ctx := context.Background()
	const prov = model.ProviderWechatNative

	t.Run("校验通过：记录校验时间，返回已校验", func(t *testing.T) {
		e := newAdminEnv(t)
		e.store.channels[prov].ValidatedAt = nil
		v, err := e.admin.ValidateChannel(ctx, 9, prov)
		if err != nil || !v.Validated || e.store.channels[prov].ValidatedAt == nil {
			t.Fatalf("%+v %v", v, err)
		}
		if e.wx.validatedCfg["api_key"] != "k" || e.wx.validatedCfg["app_id"] != "wx-app" {
			t.Fatalf("校验应拿到非敏感配置和解密后的密钥合并的完整配置：%v", e.wx.validatedCfg)
		}
	})
	t.Run("本地校验不通过返回 65009，带原因；不记校验时间", func(t *testing.T) {
		e := newAdminEnv(t)
		e.store.channels[prov].ValidatedAt = nil
		e.wx.validateErr = errors.New("缺少商户号")
		_, err := e.admin.ValidateChannel(ctx, 9, prov)
		wantPayCode(t, err, errcode.ErrPayChannelConfig.Code)
		if !strings.Contains(err.Error(), "缺少商户号") || e.store.channels[prov].ValidatedAt != nil {
			t.Fatalf("%v", err)
		}
	})
	t.Run("在线探测失败返回 65009，错误里的密钥片段被遮蔽", func(t *testing.T) {
		e := newAdminEnv(t)
		e.store.channels[prov].ValidatedAt = nil
		e.wx.probeErr = errors.New("签名错误 api_key=LEAKED-KEY")
		_, err := e.admin.ValidateChannel(ctx, 9, prov)
		wantPayCode(t, err, errcode.ErrPayChannelConfig.Code)
		if strings.Contains(err.Error(), "LEAKED-KEY") {
			t.Fatalf("错误不能带出密钥：%v", err)
		}
	})
	t.Run("渠道没配置过返回 65009", func(t *testing.T) {
		e := newAdminEnv(t)
		delete(e.store.channels, prov)
		_, err := e.admin.ValidateChannel(ctx, 9, prov)
		wantPayCode(t, err, errcode.ErrPayChannelConfig.Code)
	})

	t.Run("启用：写库、写审计", func(t *testing.T) {
		e := newAdminEnv(t)
		e.store.channels[prov].Enabled = false
		v, err := e.admin.EnableChannel(ctx, 9, prov)
		if err != nil || !v.Enabled || !e.store.channels[prov].Enabled {
			t.Fatalf("%+v %v", v, err)
		}
		if acts := e.audit.actions(); len(acts) != 1 || acts[0] != model.AdminAuditPayChannelEnable {
			t.Fatalf("%v", acts)
		}
	})
	enableFail := []struct {
		name     string
		setup    func(e *adminEnv)
		wantCode int
	}{
		{"没通过校验不能启用", func(e *adminEnv) { e.store.channels[prov].ValidatedAt = nil }, errcode.ErrPayChannelConfig.Code},
		{"没配公网地址不能启用", func(e *adminEnv) { e.kv.kv[model.SettingPaymentPublicBaseURL] = "" }, errcode.ErrPayBaseURLMissing.Code},
		{"微信要求回调地址是 https", func(e *adminEnv) { e.kv.kv[model.SettingPaymentPublicBaseURL] = "http://pay.example.com" }, errcode.ErrPayChannelConfig.Code},
	}
	for _, tt := range enableFail {
		t.Run(tt.name+"，返回 "+"错误码", func(t *testing.T) {
			e := newAdminEnv(t)
			e.store.channels[prov].Enabled = false
			tt.setup(e)
			_, err := e.admin.EnableChannel(ctx, 9, prov)
			wantPayCode(t, err, tt.wantCode)
			if e.store.channels[prov].Enabled {
				t.Fatal("不能被启用")
			}
		})
	}
	t.Run("停用：写库、写审计，返回它原本承接的支付方式", func(t *testing.T) {
		e := newAdminEnv(t)
		v, err := e.admin.DisableChannel(ctx, 9, prov)
		if err != nil || v.Enabled || e.store.channels[prov].Enabled {
			t.Fatalf("%+v %v", v, err)
		}
		if len(v.Serving) != 1 || v.Serving[0] != model.PayMethodWechat {
			t.Fatalf("应告诉前端停用影响了哪些支付方式：%+v", v.Serving)
		}
		if acts := e.audit.actions(); len(acts) != 1 || acts[0] != model.AdminAuditPayChannelDisable {
			t.Fatalf("%v", acts)
		}
	})
	t.Run("停用后下单会失败（承接渠道已停用），已有订单的回调仍能入账", func(t *testing.T) {
		e := newAdminEnv(t)
		v := e.order("idem-0001")
		if _, err := e.admin.DisableChannel(ctx, 9, prov); err != nil {
			t.Fatal(err)
		}
		_, err := e.svc.CreateOrder(context.Background(), payTestUser, &model.CreatePaymentOrderReq{ProductID: 1, Method: "wechat", IdempotencyKey: "idem-0002"}, "")
		wantPayCode(t, err, errcode.ErrPayMethodUnavail.Code)
		e.paid(v.OrderNo, 10000)
		if ack := e.notify(prov); ack.Status != 200 || e.store.orderByNo(v.OrderNo).Status != model.PayOrderCredited {
			t.Fatalf("停用渠道后已有订单的回调仍应入账：%+v", ack)
		}
	})
}
