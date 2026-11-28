CREATE INDEX idx_storyboard_split_collab ON storyboard_split_jobs (canvas_id, collaboration_epoch);
CREATE INDEX idx_generation_task_canvas_epoch ON generation_tasks (canvas_project_id, collaboration_epoch);
