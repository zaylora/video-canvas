-- 公告去掉详情（设计：docs/design/公告/公告.md §13 v6）：验收时决定公告只有一句话，不再有详情弹窗。
-- 与 0029、0031 在同一个版本里首次发布，没有已发布的代码读写 body，直接删列。
ALTER TABLE announcements DROP COLUMN body;
