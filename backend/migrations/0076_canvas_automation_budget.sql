ALTER TABLE canvas_automation_keys ADD COLUMN budget_credits integer;

ALTER TABLE canvas_automation_keys ADD CONSTRAINT canvas_automation_budget_by_scope CHECK (
    (scope = 'generate' AND budget_credits IS NOT NULL AND budget_credits > 0) OR
    (scope IN ('read', 'edit') AND budget_credits IS NULL)
);
