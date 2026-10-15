-- 画布列表封面：画布里第一张生成成功的图，由任务完成时写回（0 表示还没有，旧画布都没有）。
-- 不回填存量画布：没有封面的画布在列表页继续显示节点线框，之后在画布里生成出第一张图就会有封面。
ALTER TABLE canvas_projects ADD COLUMN cover_asset_id bigint NOT NULL DEFAULT 0;
