// Package legacymodel 是 v0.3.2 时 internal/model 的冻结副本，只给 migration.UpgradeLegacy 补齐老库用。
// 永久保留、永远不改：以后 internal/model 怎么变都不要同步到这里，结构变更走 backend/migrations 的 SQL 文件。
package legacymodel
