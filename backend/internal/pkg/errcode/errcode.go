package errcode

import (
	"fmt"
	"net/http"
)

// Error 是业务错误：Code 返回给前端，status 决定 HTTP 状态码。
type Error struct {
	Code   int
	Msg    string
	status int
}

func New(code int, msg string, status int) *Error {
	return &Error{Code: code, Msg: msg, status: status}
}

func (e *Error) Error() string {
	return fmt.Sprintf("code=%d msg=%s", e.Code, e.Msg)
}

func (e *Error) HTTPStatus() int { return e.status }

// WithMsg 返回一个替换了提示文案的副本，不修改预定义的错误。
func (e *Error) WithMsg(msg string) *Error {
	return &Error{Code: e.Code, Msg: msg, status: e.status}
}

// 通用错误 1xxxx
var (
	ErrInternal      = New(10000, "服务内部错误", http.StatusInternalServerError)
	ErrInvalidParams = New(10001, "参数错误", http.StatusBadRequest)
	ErrNotFound      = New(10002, "资源不存在", http.StatusNotFound)
	ErrUnauthorized  = New(10003, "未登录或登录已过期", http.StatusUnauthorized)
	ErrForbidden     = New(10004, "没有权限", http.StatusForbidden)
	ErrTooManyReqs   = New(10005, "请求过于频繁", http.StatusTooManyRequests)
	ErrTokenExpired  = New(10006, "登录已过期，请重新登录", http.StatusUnauthorized)
)

// 用户模块 2xxxx
var (
	ErrUserNotFound      = New(20001, "用户不存在", http.StatusNotFound)
	ErrUserExists        = New(20002, "用户名已存在", http.StatusConflict)
	ErrInvalidCredential = New(20003, "用户名或密码错误", http.StatusUnauthorized)
)

// 画布模块 3xxxx
var (
	ErrCanvasNotFound = New(30001, "画布不存在", http.StatusNotFound)
	ErrCanvasConflict = New(30002, "画布已被修改，请刷新后重试", http.StatusConflict)
	ErrCanvasPayload  = New(30003, "画布内容必须是 JSON 对象", http.StatusBadRequest)
	ErrCanvasNoChange = New(30004, "title 和 payload_json 至少传一个", http.StatusBadRequest)
)

// 生成任务 / 积分 / 素材 / AI 配置模块 4xxxx
var (
	ErrInsufficientCredits = New(40001, "积分不足", http.StatusPaymentRequired)
	ErrTooManyTasks        = New(40002, "进行中的任务已达上限，请等待完成后再试", http.StatusTooManyRequests)
	ErrModelUnavailable    = New(40003, "模型不可用或已下线", http.StatusBadRequest)
	ErrTaskNotFound        = New(40004, "任务不存在", http.StatusNotFound)
	ErrTaskNotCancelable   = New(40005, "任务已结束，无法取消", http.StatusConflict)
	ErrTaskInput           = New(40006, "生成参数不合法", http.StatusBadRequest) // 字段级错误写进 Msg
	ErrAssetNotFound       = New(40007, "素材不存在", http.StatusNotFound)
	ErrAssetInvalid        = New(40008, "素材不合法", http.StatusBadRequest)
	ErrAssetTooLarge       = New(40009, "素材超过大小限制", http.StatusRequestEntityTooLarge)
	ErrConfigInvalid       = New(40010, "配置校验未通过", http.StatusBadRequest) // 错误列表写进 Msg
	ErrConfigNotFound      = New(40011, "配置不存在", http.StatusNotFound)
	ErrConfigNoDraft       = New(40012, "没有可发布的草稿", http.StatusConflict)
	ErrSecretNotSet        = New(40013, "凭证尚未设置", http.StatusConflict)
	ErrWSTicketInvalid     = New(40014, "连接凭证无效或已过期", http.StatusUnauthorized)
)

// 协议插件 / 渠道 / 模型删除 5xxxx
var (
	ErrPluginNotFound     = New(50001, "插件不存在", http.StatusNotFound)
	ErrPluginPrecheck     = New(50002, "插件预检未通过", http.StatusBadRequest) // 问题列表写进上传响应的 issues
	ErrPluginVersionDup   = New(50003, "该插件版本号已存在，请修改 meta.version", http.StatusConflict)
	ErrPluginDisabled     = New(50004, "插件已停用", http.StatusConflict)
	ErrPluginInUse        = New(50005, "插件版本仍被渠道或进行中的任务使用，无法删除", http.StatusConflict)
	ErrPluginBuiltin      = New(50006, "内置插件不能被删除或覆盖", http.StatusConflict)
	ErrPluginTooLarge     = New(50007, "插件文件超过大小限制", http.StatusRequestEntityTooLarge)
	ErrPluginBuiltinDel   = New(50008, "内置插件不能删除，只能停用", http.StatusConflict)
	ErrChannelNotFound    = New(50011, "渠道不存在", http.StatusNotFound)
	ErrChannelExists      = New(50012, "渠道 key 已存在", http.StatusConflict)
	ErrChannelInvalid     = New(50013, "渠道配置不合法", http.StatusBadRequest) // 具体原因写进 Msg
	ErrChannelDisabled    = New(50014, "渠道已停用", http.StatusConflict)
	ErrChannelSecretUnset = New(50015, "渠道 Key 尚未设置", http.StatusConflict)
	ErrChannelInUse       = New(50016, "渠道仍被模型或进行中的任务使用，无法删除", http.StatusConflict) // 带数量的原因写进 Msg
	ErrRunnerUnavailable  = New(50021, "插件运行时暂不可用，请稍后重试", http.StatusServiceUnavailable)
	ErrPluginOpFailed     = New(50022, "插件调用失败", http.StatusBadGateway) // 连通性检查 / 导入失败，原因写进 Msg（已脱敏）
	ErrModelEnabled       = New(50031, "模型还在上线，先下线再删除", http.StatusConflict)
)

// 存储配置 / 浏览器直传 51xxx
var (
	ErrStorageNotFound        = New(51001, "存储不存在", http.StatusNotFound)
	ErrStorageNameDup         = New(51002, "存储名称已存在", http.StatusConflict)
	ErrStorageInvalid         = New(51003, "存储配置不合法", http.StatusBadRequest) // 具体原因写进 Msg
	ErrStorageInUse           = New(51004, "存储仍被素材或进行中的上传使用，无法删除", http.StatusConflict)
	ErrStorageFieldLocked     = New(51005, "该存储已有素材引用，定位字段不能修改", http.StatusConflict) // 被锁字段写进 Msg
	ErrStorageBuiltin         = New(51006, "内置存储不能修改或删除", http.StatusConflict)
	ErrStorageCheckFailed     = New(51007, "存储连接测试未通过", http.StatusBadRequest) // 失败步骤与原因写进 Msg
	ErrStorageUnavailable     = New(51008, "存储暂不可用，请稍后重试", http.StatusBadGateway)
	ErrStorageVersionConflict = New(51009, "存储配置已被其他人修改，请刷新后重试", http.StatusConflict)
	ErrStorageNotChecked      = New(51010, "最近一次连接测试未通过，不能设为默认存储", http.StatusConflict)
	ErrStorageIsDefault       = New(51011, "默认存储不能删除，请先把其他存储设为默认", http.StatusConflict)
	ErrStorageSecretUnset     = New(51012, "存储密钥尚未设置", http.StatusConflict)
	ErrUploadIntentNotFound   = New(51021, "上传申请不存在或已过期", http.StatusNotFound)
	ErrUploadSizeMismatch     = New(51022, "上传的文件大小与申请不一致", http.StatusBadRequest)
	ErrUploadDirectDisabled   = New(51023, "当前存储未开启浏览器直传", http.StatusConflict)
)

// 图片处理服务 52xxx
var (
	ErrProcessorNotFound        = New(52001, "图片处理服务不存在", http.StatusNotFound)
	ErrProcessorNameDup         = New(52002, "图片处理服务名称已存在", http.StatusConflict)
	ErrProcessorInvalid         = New(52003, "图片处理服务配置不合法", http.StatusBadRequest) // 具体原因写进 Msg
	ErrProcessorVersionConflict = New(52004, "图片处理服务已被其他人修改，请刷新后重试", http.StatusConflict)
	ErrProcessorStorageMismatch = New(52005, "处理服务厂商与存储不匹配", http.StatusBadRequest) // 具体原因写进 Msg
	ErrProcessorStorageBound    = New(52006, "该存储已被另一个已发布的图片处理服务绑定", http.StatusConflict)
	ErrProcessorNotChecked      = New(52007, "当前草稿还没有通过校验与试跑，不能发布", http.StatusConflict)
	ErrProcessorPublished       = New(52008, "已发布的图片处理服务不能删除，请先停用", http.StatusConflict)
	ErrProcessorNoPrevious      = New(52009, "没有可回滚的版本", http.StatusConflict)
	ErrProcessorNotPublished    = New(52010, "图片处理服务不是已发布状态", http.StatusConflict)
)

// 用户管理与注册 53xxx
var (
	ErrRegisterClosed      = New(53001, "暂未开放注册", http.StatusForbidden)
	ErrEmailExists         = New(53002, "该邮箱已注册", http.StatusConflict)
	ErrCodeInvalid         = New(53003, "验证码错误或已过期", http.StatusBadRequest)
	ErrAccountDisabled     = New(53004, "账号已被停用，请联系管理员", http.StatusForbidden)
	ErrCreditAdjustInvalid = New(53005, "积分调整不合法", http.StatusBadRequest)   // 原因写进 Msg
	ErrSMTPInvalid         = New(53006, "邮件服务配置不合法", http.StatusBadRequest) // 原因写进 Msg
	ErrSMTPSendFailed      = New(53007, "邮件发送失败", http.StatusBadGateway)    // 原因写进 Msg（已脱敏）
	ErrSMTPNotConfigured   = New(53008, "邮件服务未配置", http.StatusConflict)
)

// 登录页展示 54xxx
var (
	ErrShowcaseNotFound      = New(54001, "登录页展示条目不存在", http.StatusNotFound)
	ErrShowcaseInvalid       = New(54002, "登录页展示参数不合法", http.StatusBadRequest)          // 具体原因写进 Msg
	ErrShowcaseOrderMismatch = New(54003, "排序列表与现有条目不一致，请刷新后重试", http.StatusBadRequest) // 多了、少了或重复了条目
)

// 画布 Agent 模块 6xxxx
var (
	ErrAgentRunActive      = New(60001, "这个画布上已有运行中的 Agent", http.StatusConflict)
	ErrAgentModelNA        = New(60002, "Agent 模型不可用", http.StatusBadRequest)
	ErrAgentState          = New(60003, "当前状态不允许这个操作", http.StatusConflict)
	ErrAgentApprovalGone   = New(60004, "审批已处理或已过期", http.StatusConflict)
	ErrAgentUnavailable    = New(60005, "Agent 暂不可用，请稍后重试", http.StatusServiceUnavailable)
	ErrAgentWriteConflict  = New(60006, "画布写入冲突，重试仍未成功", http.StatusConflict)
	ErrAgentAlreadyUndone  = New(60007, "本轮已撤销", http.StatusConflict)
	ErrAgentOverBudget     = New(60008, "超出本轮积分预算", http.StatusPaymentRequired)
	ErrAgentSessionLimit   = New(60010, "这个画布的会话数量已达上限", http.StatusConflict)
	ErrAgentSessionMissing = New(60011, "会话不存在", http.StatusNotFound)
	ErrAgentRunMissing     = New(60012, "运行不存在", http.StatusNotFound)
	ErrAgentApprovalMiss   = New(60013, "审批不存在", http.StatusNotFound)
)

// Agent 技能管理 61xxx
var (
	ErrSkillImportGone     = New(61001, "导入已过期或不存在，请重新上传", http.StatusNotFound)
	ErrSkillPrecheckFailed = New(61002, "预检未通过，不能导入", http.StatusConflict)
	ErrSkillBuiltinName    = New(61003, "与内置技能同名，不能导入", http.StatusConflict)
	ErrSkillNotFound       = New(61004, "技能不存在", http.StatusNotFound)
	ErrSkillVersionMissing = New(61005, "技能版本不存在", http.StatusNotFound)
	ErrSkillSameContent    = New(61006, "内容与已有版本相同", http.StatusConflict)
	ErrSkillBuiltin        = New(61007, "内置技能不可修改", http.StatusConflict)
	ErrSkillActiveVersion  = New(61008, "生效版本不能删除", http.StatusConflict)
	ErrSkillCannotEnable   = New(61009, "无法启用", http.StatusConflict) // 原因写进 Msg
	ErrSkillNeedDisable    = New(61010, "技能需先停用才能删除", http.StatusConflict)
	ErrSkillTooLarge       = New(61011, "技能包超过大小限制", http.StatusRequestEntityTooLarge)
)
