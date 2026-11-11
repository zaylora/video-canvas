-- 公告去掉跳转链接（设计：docs/design/公告/公告.md §13 v4）：验收时决定通栏不放「了解更多」、后台不填链接。
-- 0029 与本文件在同一个版本里首次发布，没有任何已发布的代码读写 link_url，所以不必分两个版本做 Expand–Contract，直接删列。
ALTER TABLE announcements DROP COLUMN link_url;
