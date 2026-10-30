CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_task_user_canvas_node ON generation_tasks USING btree (user_id, canvas_project_id, node_id);
