CREATE TABLE canvas_generation_budgets (
    id bigserial PRIMARY KEY,
    canvas bigint NOT NULL,
    "user" bigint NOT NULL,
    epoch bigint NOT NULL,
    key varchar(64) NOT NULL,
    maximum bigint NOT NULL
);
CREATE UNIQUE INDEX idx_canvas_generation_budget ON canvas_generation_budgets (canvas, "user", epoch, key);
ALTER TABLE generation_tasks ADD COLUMN budget_group_id bigint;
ALTER TABLE storyboard_split_jobs ADD COLUMN budget_group_id bigint;
CREATE INDEX idx_generation_tasks_budget_group_id ON generation_tasks (budget_group_id) WHERE budget_group_id IS NOT NULL;
CREATE INDEX idx_storyboard_split_jobs_budget_group_id ON storyboard_split_jobs (budget_group_id) WHERE budget_group_id IS NOT NULL;
