-- 公告去掉「重要」（设计：docs/design/公告/公告.md §13 v9）：验收时决定所有公告都能关闭，顺序只看后台排序。
-- 与 0029、0031、0032 在同一个版本里首次发布，没有已发布的代码读写 important，直接删列。
ALTER TABLE announcements DROP COLUMN important;
