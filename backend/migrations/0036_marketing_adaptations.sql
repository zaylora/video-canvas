-- 营销出片前的提示词转写：脚本按格式存转写稿；转写扣费的流水关联脚本（同一脚本可多次转写，不唯一）。
ALTER TABLE marketing_scripts ADD COLUMN adaptations jsonb NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE credit_ledger ADD COLUMN marketing_script_id bigint;
CREATE INDEX idx_ledger_marketing_script ON credit_ledger (marketing_script_id) WHERE marketing_script_id IS NOT NULL;
