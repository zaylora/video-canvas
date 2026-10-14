// Package migrations 存放版本化的数据库迁移 SQL，编译时内嵌进二进制，由 internal/migration 在启动时执行。
//
// 规则（详见 backend/AGENTS.md「数据库迁移」）：
//   - 文件名 NNNN_小写下划线描述.sql，四位编号连续递增；.notx.sql 不包事务，只能有一条语句。
//   - 已发布的文件永远不改，要修正就新增一个文件。
//   - 改了 internal/model 的表结构，必须同时新增迁移文件（结构一致性测试会检查）。
package migrations

import "embed"

// FS 是全部迁移文件。
//
//go:embed *.sql
var FS embed.FS
