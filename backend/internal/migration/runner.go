// Package migration 在服务启动时执行版本化的 SQL 迁移：
// 迁移文件内嵌在二进制里（backend/migrations），执行记录写在 schema_migrations，
// 多个实例同时启动时用 PostgreSQL advisory lock 保证只有一个实例真正执行。
// 迁移只向前；已发布的文件不能再改，改动会在启动时因 checksum 不一致被拒绝。
package migration

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"video-canvas/migrations"
)

const (
	// BaselineFile 是基线迁移：接入迁移器之前（v0.3.2）的完整库结构。
	BaselineFile = "0001_baseline.sql"
	// legacyProbeTable 用来识别接入迁移器之前由 AutoMigrate 建出的老库：有这张表却没有基线记录。
	legacyProbeTable = "users"
	// lockKey 是迁移用的 advisory lock 键，库内唯一即可。
	lockKey int64 = 7_402_691_588_213_904_001
	// defaultLockWait 是等其他实例做完迁移的最长时间。
	defaultLockWait = 10 * time.Minute
	// statementLockTimeout 限制单个迁移等表锁的时间，避免 ALTER TABLE 卡住启动。
	statementLockTimeout = "30s"
)

// fileNamePattern 是迁移文件名规则：四位编号_小写下划线描述，.notx.sql 表示不包事务。
var fileNamePattern = regexp.MustCompile(`^[0-9]{4}_[a-z0-9_]+(\.notx)?\.sql$`)

// Options 是 Run 的参数，零值字段取默认值。
type Options struct {
	FS               fs.FS                             // 迁移文件；nil 时用内嵌的 migrations.FS
	AppVersion       string                            // 当前程序版本，写进执行记录
	AllowNewerSchema bool                              // 库里有本程序不认识的迁移时仍然启动（只在确认那些迁移向后兼容时打开）
	LockWait         time.Duration                     // 等锁上限，默认 10 分钟
	Legacy           func(*gorm.DB, *zap.Logger) error // 老库补齐到基线；nil 时用 UpgradeLegacy
	Logger           *zap.Logger
}

// FileStatus 是一个迁移文件的执行状态。
type FileStatus struct {
	Filename   string    `json:"filename"`
	Applied    bool      `json:"applied"`
	AppliedAt  time.Time `json:"applied_at,omitempty"`
	AppVersion string    `json:"app_version,omitempty"`
	DurationMs int64     `json:"duration_ms,omitempty"`
	Modified   bool      `json:"modified"` // 已执行，但文件内容和执行时不一样
	Unknown    bool      `json:"unknown"`  // 库里有记录，程序里没有这个文件（库比程序新）
}

type migrationFile struct {
	name     string
	content  string
	checksum string
	noTx     bool
}

type record struct {
	Filename   string
	Checksum   string
	AppVersion string
	DurationMs int64
	AppliedAt  time.Time
}

// Run 执行全部未执行的迁移。任何一步失败都返回错误，调用方应中止启动。
func Run(ctx context.Context, db *gorm.DB, opts Options) error {
	opts = withDefaults(opts)
	files, err := loadFiles(opts.FS)
	if err != nil {
		return err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	// advisory lock 是会话级的：加锁、执行、解锁必须在同一条连接上
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		return fmt.Errorf("获取迁移连接失败: %w", err)
	}
	defer conn.Close()

	if err := acquireLock(ctx, conn, opts); err != nil {
		return err
	}
	defer releaseLock(conn)

	if _, err := conn.ExecContext(ctx, createRecordTable); err != nil {
		return fmt.Errorf("创建 schema_migrations 失败: %w", err)
	}
	records, err := loadRecords(ctx, conn)
	if err != nil {
		return err
	}
	if err := checkRecords(files, records, opts); err != nil {
		return err
	}
	if err := adoptLegacy(ctx, conn, db, files, records, opts); err != nil {
		return err
	}
	for _, f := range files {
		if _, ok := records[f.name]; ok {
			continue
		}
		start := time.Now()
		if err := apply(ctx, conn, f, opts.AppVersion); err != nil {
			return fmt.Errorf("执行迁移 %s 失败: %w", f.name, err)
		}
		opts.Logger.Info("数据库迁移完成", zap.String("file", f.name), zap.Duration("cost", time.Since(start)))
	}
	return nil
}

// Status 列出每个迁移文件的执行状态，以及库里有、程序里没有的记录。只读，不建表。
func Status(ctx context.Context, db *gorm.DB, fsys fs.FS) ([]FileStatus, error) {
	if fsys == nil {
		fsys = migrations.FS
	}
	files, err := loadFiles(fsys)
	if err != nil {
		return nil, err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	records := map[string]record{}
	var exists bool
	if err := conn.QueryRowContext(ctx, "SELECT to_regclass('schema_migrations') IS NOT NULL").Scan(&exists); err != nil {
		return nil, err
	}
	if exists {
		if records, err = loadRecords(ctx, conn); err != nil {
			return nil, err
		}
	}

	out := make([]FileStatus, 0, len(files))
	known := map[string]bool{}
	for _, f := range files {
		known[f.name] = true
		st := FileStatus{Filename: f.name}
		if r, ok := records[f.name]; ok {
			st.Applied, st.AppliedAt, st.AppVersion, st.DurationMs = true, r.AppliedAt, r.AppVersion, r.DurationMs
			st.Modified = r.Checksum != f.checksum
		}
		out = append(out, st)
	}
	for _, r := range sortedRecords(records) {
		if !known[r.Filename] {
			out = append(out, FileStatus{Filename: r.Filename, Applied: true, AppliedAt: r.AppliedAt, AppVersion: r.AppVersion, DurationMs: r.DurationMs, Unknown: true})
		}
	}
	return out, nil
}

const createRecordTable = `CREATE TABLE IF NOT EXISTS schema_migrations (
	filename    TEXT PRIMARY KEY,
	checksum    TEXT NOT NULL,
	app_version TEXT NOT NULL,
	duration_ms BIGINT NOT NULL DEFAULT 0,
	applied_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
)`

func withDefaults(opts Options) Options {
	if opts.FS == nil {
		opts.FS = migrations.FS
	}
	if opts.LockWait <= 0 {
		opts.LockWait = defaultLockWait
	}
	if opts.Legacy == nil {
		opts.Legacy = UpgradeLegacy
	}
	if opts.Logger == nil {
		opts.Logger = zap.NewNop()
	}
	if opts.AppVersion == "" {
		opts.AppVersion = "unknown"
	}
	return opts
}

// loadFiles 读取全部 .sql 迁移文件并按文件名排序；文件名不合规直接报错，不执行任何迁移。
func loadFiles(fsys fs.FS) ([]migrationFile, error) {
	names, err := fs.Glob(fsys, "*.sql")
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	files := make([]migrationFile, 0, len(names))
	seen := map[string]string{}
	for _, name := range names {
		if !fileNamePattern.MatchString(name) {
			return nil, fmt.Errorf("迁移文件名 %s 不合规：应为 NNNN_小写下划线描述.sql 或 .notx.sql", name)
		}
		if prev, ok := seen[name[:4]]; ok {
			return nil, fmt.Errorf("迁移文件 %s 与 %s 编号重复", prev, name)
		}
		seen[name[:4]] = name
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(data)
		files = append(files, migrationFile{
			name:     name,
			content:  string(data),
			checksum: hex.EncodeToString(sum[:]),
			noTx:     strings.HasSuffix(name, ".notx.sql"),
		})
	}
	return files, nil
}

// acquireLock 循环尝试拿 advisory lock：其他实例正在迁移时等它做完，最多等 opts.LockWait。
func acquireLock(ctx context.Context, conn *sql.Conn, opts Options) error {
	ctx, cancel := context.WithTimeout(ctx, opts.LockWait)
	defer cancel()
	start := time.Now()
	warned := false
	for {
		var ok bool
		if err := conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", lockKey).Scan(&ok); err != nil {
			return fmt.Errorf("获取迁移锁失败: %w", err)
		}
		if ok {
			return nil
		}
		if !warned && time.Since(start) > 5*time.Second {
			opts.Logger.Warn("另一个实例正在执行数据库迁移，等待它完成")
			warned = true
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("等待迁移锁超时（%s），可能有实例卡在迁移中: %w", opts.LockWait, ctx.Err())
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func releaseLock(conn *sql.Conn) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = conn.ExecContext(ctx, "SELECT pg_advisory_unlock($1)", lockKey)
}

func loadRecords(ctx context.Context, conn *sql.Conn) (map[string]record, error) {
	rows, err := conn.QueryContext(ctx, "SELECT filename, checksum, app_version, duration_ms, applied_at FROM schema_migrations")
	if err != nil {
		return nil, fmt.Errorf("读取迁移记录失败: %w", err)
	}
	defer rows.Close()
	records := map[string]record{}
	for rows.Next() {
		var r record
		if err := rows.Scan(&r.Filename, &r.Checksum, &r.AppVersion, &r.DurationMs, &r.AppliedAt); err != nil {
			return nil, err
		}
		records[r.Filename] = r
	}
	return records, rows.Err()
}

func sortedRecords(records map[string]record) []record {
	out := make([]record, 0, len(records))
	for _, r := range records {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Filename < out[j].Filename })
	return out
}

// checkRecords 校验已执行的记录：文件被改过一律拒绝；库里有程序不认识的迁移（库比程序新）默认拒绝。
func checkRecords(files []migrationFile, records map[string]record, opts Options) error {
	known := map[string]bool{}
	for _, f := range files {
		known[f.name] = true
		if r, ok := records[f.name]; ok && r.Checksum != f.checksum {
			return fmt.Errorf("迁移文件 %s 在执行后被修改过（checksum 不一致）；已发布的迁移不能改，请新增一个迁移文件", f.name)
		}
	}
	for _, r := range sortedRecords(records) {
		if known[r.Filename] {
			continue
		}
		if !opts.AllowNewerSchema {
			return fmt.Errorf("数据库已执行过本程序没有的迁移 %s（由 v%s 执行），当前程序是 v%s；"+
				"请升级程序，或在确认这些迁移向后兼容后设置 database.allow_newer_schema=true", r.Filename, r.AppVersion, opts.AppVersion)
		}
		opts.Logger.Warn("数据库有本程序没有的迁移，已按 allow_newer_schema 放行",
			zap.String("file", r.Filename), zap.String("applied_by", r.AppVersion))
	}
	return nil
}

// adoptLegacy 处理接入迁移器之前的老库：还没有基线记录、但已经有业务表时，
// 先用冻结的旧迁移链把它补齐到基线结构，再只记录基线（不执行基线 SQL），之后的迁移照常执行。
func adoptLegacy(ctx context.Context, conn *sql.Conn, db *gorm.DB, files []migrationFile, records map[string]record, opts Options) error {
	if _, ok := records[BaselineFile]; ok {
		return nil
	}
	var baseline *migrationFile
	for i := range files {
		if files[i].name == BaselineFile {
			baseline = &files[i]
		}
	}
	if baseline == nil {
		return nil
	}
	var legacy bool
	if err := conn.QueryRowContext(ctx, "SELECT to_regclass($1) IS NOT NULL", legacyProbeTable).Scan(&legacy); err != nil {
		return err
	}
	if !legacy {
		return nil
	}
	opts.Logger.Info("检测到接入迁移器之前的数据库，先补齐到基线结构")
	start := time.Now()
	if err := opts.Legacy(db.WithContext(ctx), opts.Logger); err != nil {
		return fmt.Errorf("老库补齐到基线失败: %w", err)
	}
	if err := insertRecord(ctx, conn, baseline, opts.AppVersion, time.Since(start)); err != nil {
		return err
	}
	records[BaselineFile] = record{Filename: BaselineFile, Checksum: baseline.checksum}
	return nil
}

// apply 执行一个迁移文件并写记录。普通文件在一个事务里执行，失败整体回滚；
// .notx.sql 不包事务（CREATE INDEX CONCURRENTLY 等），只能有一条语句，并且应当幂等。
func apply(ctx context.Context, conn *sql.Conn, f migrationFile, appVersion string) error {
	start := time.Now()
	if f.noTx {
		if _, err := conn.ExecContext(ctx, f.content); err != nil {
			return err
		}
		return insertRecord(ctx, conn, &f, appVersion, time.Since(start))
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, "SET LOCAL lock_timeout = '"+statementLockTimeout+"'"); err != nil {
		return err
	}
	// 没有参数时 pgx 走简单查询协议，一个文件里可以放多条语句
	if _, err := tx.ExecContext(ctx, f.content); err != nil {
		return err
	}
	if err := insertRecord(ctx, tx, &f, appVersion, time.Since(start)); err != nil {
		return err
	}
	return tx.Commit()
}

type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func insertRecord(ctx context.Context, conn execer, f *migrationFile, appVersion string, cost time.Duration) error {
	_, err := conn.ExecContext(ctx,
		"INSERT INTO schema_migrations (filename, checksum, app_version, duration_ms) VALUES ($1, $2, $3, $4)",
		f.name, f.checksum, appVersion, cost.Milliseconds())
	if err != nil {
		return fmt.Errorf("写入迁移记录 %s 失败: %w", f.name, err)
	}
	return nil
}
