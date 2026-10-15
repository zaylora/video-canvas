-- 渠道不再绑定协议插件：协议、插件版本与协议设置项改放在模型配置的 channels[0].protocol 里。
-- 设计见 docs/design/渠道模型重构/渠道模型重构.md。
--
-- 本次升级不迁移存量数据（用户已确认）：渠道、模型、渠道 Key 一起清空，插件与历史任务保留。
-- 进行中的任务按快照执行，清空后会因取不到渠道 Key 而失败，升级前请等任务跑完。
DELETE FROM ai_config_revisions WHERE target = 'model';
DELETE FROM ai_models;
DELETE FROM ai_secrets WHERE name LIKE 'channel:%';
DELETE FROM ai_channels;

-- 新增：自定义请求头、渠道顺序。
ALTER TABLE ai_channels ADD COLUMN headers_json json NOT NULL;
ALTER TABLE ai_channels ADD COLUMN sort bigint NOT NULL DEFAULT 100;

-- 先加后删：旧四列（plugin_key / plugin_version_id / allow_credentials / settings_json）代码不再读写，
-- 本版本只补默认值让新建渠道不用写它们，保留到下个版本再删，这样退回上一个版本的镜像仍能用当前的库。
ALTER TABLE ai_channels ALTER COLUMN plugin_key SET DEFAULT '';
ALTER TABLE ai_channels ALTER COLUMN plugin_version_id SET DEFAULT 0;
ALTER TABLE ai_channels ALTER COLUMN settings_json SET DEFAULT '{}';
