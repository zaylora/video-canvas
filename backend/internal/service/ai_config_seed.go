package service

import (
	"context"
	"errors"

	"go.uber.org/zap"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/repository"
)

// aiRunningHubSecretName 是种子 RunningHub 平台引用的凭证名，需要运营在管理端设置。
const aiRunningHubSecretName = "runninghub_api_key"

// aiRunningHubSeed 是 RunningHub 平台的种子配置（见平台协议配置化设计 5.3 节）。
// 响应字段路径以试跑结果为准，运营可以在管理端修改草稿后重新发布。
const aiRunningHubSeed = `{
  "dsl": 1,
  "key": "runninghub",
  "name": "RunningHub",
  "base_url": "https://www.runninghub.cn",
  "allowed_hosts": ["www.runninghub.cn", "*.runninghub.cn", "*.runninghub.ai", "*.myqcloud.com"],
  "auth": { "type": "bearer", "secret": "runninghub_api_key" },
  "rate_limit": { "rps": 5, "max_concurrency": 20 },
  "poll": { "first_delay": "10s", "interval": "5s", "max_interval": "15s", "jitter": 0.2 },
  "operations": {
    "upload": {
      "method": "POST",
      "path": "/openapi/v2/media/upload/binary",
      "encoding": { "type": "multipart", "file_field": "file" },
      "success": "status == 200 && resp.code == 0",
      "extract": { "ref": "resp.data.fileName" }
    },
    "submit": {
      "method": "POST",
      "path": "/openapi/v2/run/ai-app/${ model.params.webappId }",
      "encoding": { "type": "json" },
      "body": {
        "nodeInfoList": "${ model.mapping.nodeInfoList }",
        "instanceType": "${ model.params.instanceType ?? 'default' }",
        "webhookUrl": "${ ctx.webhook_url }"
      },
      "success": "status == 200 && resp.taskId != nil && len(parseJSON(resp.promptTips ?? '{}')?.node_errors ?? {}) == 0",
      "extract": { "provider_task_id": "resp.taskId" }
    },
    "query": {
      "method": "POST",
      "path": "/openapi/v2/query",
      "encoding": { "type": "json" },
      "body": { "taskId": "${ task.provider_task_id }" },
      "success": "status == 200",
      "extract": {
        "status": "resp.status",
        "outputs": "map(resp.results ?? [], {url: .url, type: .outputType, node: .nodeId, text: .text})",
        "error_code": "resp.errorCode",
        "error_message": "resp.errorMessage",
        "provider_cost": "resp.usage?.consumeMoney"
      }
    },
    "cancel": null
  },
  "status_map": {
    "QUEUED": "queued",
    "RUNNING": "running",
    "SUCCESS": "succeeded",
    "FAILED": "failed",
    "_default": "running"
  },
  "error_rules": [
    { "when": "status == 429 || status >= 500", "class": "retryable" },
    { "when": "matches(toString(resp.errorMessage), '(?i)balance|insufficient|余额')", "class": "provider_balance" },
    { "when": "matches(toString(resp.errorMessage), '(?i)sensitive|nsfw|审核|违规')", "class": "moderation" },
    { "when": "true", "class": "terminal" }
  ],
  "webhook": {
    "verify": { "type": "path_secret" },
    "task_id": "req.taskId"
  }
}`

// SeedDefaults 启动时调用：如果 runninghub 平台不存在，就用设计文档的配置创建并直接发布。幂等，已存在时什么都不改。
// 种子直接写库并跳过“凭证已设置”检查（首次部署时运营还没来得及设置凭证），只记录日志提醒去设置。
// 种子不走 ConfigValidator 校验：它是代码里的固定文本；加载进 Registry 时仍会解析，解析不过会被忽略并记录错误。
func (s *AIConfigService) SeedDefaults(ctx context.Context) error {
	// 1. 已存在就不动：运营可能已经修改并发布了自己的版本，种子不能覆盖
	_, err := s.repo.GetProviderPointer(ctx, "runninghub")
	if err == nil {
		s.warnIfSecretMissing(ctx)
		return nil
	}
	if !errors.Is(err, repository.ErrNotFound) {
		return err
	}

	// 2. 保存为草稿再直接发布（系统操作，操作人为 0）
	ptr := repository.ConfigPointer{Target: model.ConfigTargetProvider, Key: "runninghub", Name: "RunningHub"}
	rev, err := s.repo.SaveDraft(ctx, repository.SaveDraftInput{
		Pointer: ptr, Body: []byte(aiRunningHubSeed), CreatedBy: 0, Note: "系统种子",
	})
	if err != nil {
		return err
	}
	if _, err := s.repo.PublishDraft(ctx, ptr, rev.ID); err != nil {
		return err
	}
	logger.Info("已写入 RunningHub 平台种子配置并发布", zap.Uint64("revision_id", rev.ID))

	// 3. 热生效并提醒设置凭证
	s.notifyChanged(ctx, "seed runninghub")
	s.warnIfSecretMissing(ctx)
	return nil
}

// warnIfSecretMissing 凭证没设置时打一条醒目的日志，提醒运营去管理端设置。
func (s *AIConfigService) warnIfSecretMissing(ctx context.Context) {
	if _, err := s.repo.GetSecret(ctx, aiRunningHubSecretName); errors.Is(err, repository.ErrNotFound) {
		logger.Warn("RunningHub 凭证尚未设置，生成任务将无法提交；请管理员调用 PUT /api/v1/admin/ai/secrets/" + aiRunningHubSecretName + " 设置")
	}
}
