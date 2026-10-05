package service

import (
	"context"
	"encoding/base64"
	"strconv"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
)

// 后台用户详情的三个记录列表：游标分页，按 id 倒序。

const (
	defaultRecordLimit = 20  // 记录列表默认每页条数
	maxRecordLimit     = 100 // 记录列表每页条数上限
)

// itoa 把整数转成十进制字符串（拼错误文案用）。
func itoa(n int) string { return strconv.Itoa(n) }

// encodeCursor 把“上一页最后一条的 id”编码成不透明游标（base64url），前端只需原样传回。
func encodeCursor(id uint64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatUint(id, 10)))
}

// decodeCursor 还原游标；空串表示第一页（返回 0）。无法解析或解出 0 都按参数错误处理。
func decodeCursor(c string) (uint64, error) {
	if c == "" {
		return 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(c)
	if err == nil {
		var id uint64
		if id, err = strconv.ParseUint(string(raw), 10, 64); err == nil && id > 0 {
			return id, nil
		}
	}
	return 0, errcode.ErrInvalidParams.WithMsg("游标无效")
}

// recordLimit 修正每页条数：<=0 取默认 20，超过 100 按 100。
func recordLimit(n int) int {
	if n <= 0 {
		return defaultRecordLimit
	}
	return min(n, maxRecordLimit)
}

// toCursorPage 把“多取了 1 条”的查询结果裁成一页：多出来的那条说明还有下一页，游标指向本页最后一条。
// 返回的 Items 永远不是 nil，没有更多时 NextCursor 为空串。
func toCursorPage[T any](rows []T, limit int, idOf func(T) uint64) *model.CursorPage[T] {
	page := &model.CursorPage[T]{Items: rows}
	if len(rows) > limit {
		page.Items = rows[:limit]
		page.NextCursor = encodeCursor(idOf(rows[limit-1]))
	}
	if page.Items == nil {
		page.Items = []T{}
	}
	return page
}

// recordQuery 做三个列表共有的前置步骤：用户必须存在、解析游标、修正 limit。返回的 limit 已加 1（多取一条判断是否有下一页）。
func (s *AdminUserService) recordQuery(ctx context.Context, userID uint64, cursor string, limit int) (before uint64, pageSize int, err error) {
	if _, err := s.loadTarget(ctx, userID); err != nil {
		return 0, 0, err
	}
	if before, err = decodeCursor(cursor); err != nil {
		return 0, 0, err
	}
	return before, recordLimit(limit), nil
}

// taskStatuses 把生成记录的筛选值映射成任务状态集合：failed 包含 expired 与 canceled（取消的任务也是“没出结果”，
// 否则它们只能在“全部”里看到）；all / 空不筛。
func taskStatuses(filter string) ([]string, error) {
	switch filter {
	case "", "all":
		return nil, nil
	case "success":
		return []string{model.TaskSucceeded}, nil
	case "failed":
		return []string{model.TaskFailed, model.TaskExpired, model.TaskCanceled}, nil
	case "running":
		return model.ActiveTaskStatuses, nil
	}
	return nil, errcode.ErrInvalidParams.WithMsg("状态筛选值不合法")
}

// ListTasks 返回用户的生成记录（排除试跑），游标分页。用户不存在返回 ErrUserNotFound。
func (s *AdminUserService) ListTasks(ctx context.Context, userID uint64, req *model.ListUserTasksReq) (*model.CursorPage[model.AdminTaskItem], error) {
	// 1. 用户存在、游标与 limit 修正
	before, limit, err := s.recordQuery(ctx, userID, req.Cursor, req.Limit)
	if err != nil {
		return nil, err
	}
	// 2. 状态筛选映射
	statuses, err := taskStatuses(req.Status)
	if err != nil {
		return nil, err
	}
	// 3. 多取一条判断是否还有下一页
	rows, err := s.repo.ListTasks(ctx, repository.TaskRecordFilter{UserID: userID, Statuses: statuses, BeforeID: before, Limit: limit + 1})
	if err != nil {
		return nil, err
	}
	return toCursorPage(rows, limit, func(r model.AdminTaskItem) uint64 { return r.ID }), nil
}

// ListLedger 返回用户的积分流水，游标分页。type=admin 只看管理员调整；task 看 freeze / settle / refund（不含 initial）；all 全部。
func (s *AdminUserService) ListLedger(ctx context.Context, userID uint64, req *model.ListUserLedgerReq) (*model.CursorPage[model.AdminLedgerItem], error) {
	// 1. 用户存在、游标与 limit 修正
	before, limit, err := s.recordQuery(ctx, userID, req.Cursor, req.Limit)
	if err != nil {
		return nil, err
	}
	// 2. 类型筛选映射
	var types []string
	switch req.Type {
	case "", "all":
	case "admin":
		types = []string{model.LedgerAdminAdjust}
	case "task":
		types = []string{model.LedgerFreeze, model.LedgerSettle, model.LedgerRefund}
	default:
		return nil, errcode.ErrInvalidParams.WithMsg("类型筛选值不合法")
	}
	// 3. 多取一条判断是否还有下一页
	rows, err := s.repo.ListLedger(ctx, repository.LedgerRecordFilter{UserID: userID, Types: types, BeforeID: before, Limit: limit + 1})
	if err != nil {
		return nil, err
	}
	// 4. 金额映射成带符号的“对可用积分的影响”（只改响应，不动库里的值，对账仍用库里原值）
	for i := range rows {
		rows[i].Amount = signedLedgerAmount(rows[i].Type, rows[i].Amount)
	}
	return toCursorPage(rows, limit, func(r model.AdminLedgerItem) uint64 { return r.ID }), nil
}

// signedLedgerAmount 把流水金额统一成带符号的“对用户可用积分的影响”，前端按正负着色：
// 库里 freeze / settle / refund / initial 都存正数，只有 admin_adjust 自带正负号，所以：
//   - freeze 为负（冻结占用可用积分）；
//   - settle 为负（实际扣减；它之前的 freeze 已占用过，这里只是展示流水，不做抵扣）；
//   - refund、initial 为正；
//   - admin_adjust 保持原值。
//
// 其他未知类型也保持原值。
func signedLedgerAmount(typ string, amount int) int {
	switch typ {
	case model.LedgerFreeze, model.LedgerSettle:
		return -abs(amount)
	case model.LedgerRefund, model.LedgerInitial:
		return abs(amount)
	}
	return amount
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// ListLogins 返回用户的登录 / 注册记录，游标分页。result=fail 只看密码错误与账号停用（badpw / blocked）。
func (s *AdminUserService) ListLogins(ctx context.Context, userID uint64, req *model.ListUserLoginsReq) (*model.CursorPage[model.AdminLoginItem], error) {
	// 1. 用户存在、游标与 limit 修正
	before, limit, err := s.recordQuery(ctx, userID, req.Cursor, req.Limit)
	if err != nil {
		return nil, err
	}
	// 2. 结果筛选映射
	var results []string
	switch req.Result {
	case "", "all":
	case "fail":
		results = []string{model.LoginResultBadPw, model.LoginResultBlocked}
	default:
		return nil, errcode.ErrInvalidParams.WithMsg("结果筛选值不合法")
	}
	// 3. 多取一条判断是否还有下一页
	rows, err := s.repo.ListLogins(ctx, repository.LoginRecordFilter{UserID: userID, Results: results, BeforeID: before, Limit: limit + 1})
	if err != nil {
		return nil, err
	}
	return toCursorPage(rows, limit, func(r model.AdminLoginItem) uint64 { return r.ID }), nil
}
