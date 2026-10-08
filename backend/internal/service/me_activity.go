package service

import (
	"context"
	"time"
	_ "time/tzdata" // 内置时区数据：容器镜像里可能没有系统 tzdata，没有它 LoadLocation 会失败

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
)

// defaultActivityTZ 是热力图时区非法或缺省时的回落时区（与数据库连接的时区一致）。
const defaultActivityTZ = "Asia/Shanghai"

// activityDateLayout 是热力图日期的格式。
const activityDateLayout = "2006-01-02"

// activityLocation 解析浏览器传来的 IANA 时区；空串、"Local"（服务器本地时区，对用户没有意义）与无法解析的值都回落到上海。
// 故意不报错：时区只影响分天，回落总比整块热力图加载失败好。
func activityLocation(tz string) *time.Location {
	if tz != "" && tz != "Local" {
		if loc, err := time.LoadLocation(tz); err == nil {
			return loc
		}
	}
	loc, err := time.LoadLocation(defaultActivityTZ)
	if err != nil {
		return time.UTC // 内置了 tzdata，理论上不会走到这里
	}
	return loc
}

// dateOf 返回 t 在 loc 时区的那一天的 0 点。
func dateOf(t time.Time, loc *time.Location) time.Time {
	y, m, d := t.In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, loc)
}

// Activity 返回热力图数据（GET /me/activity）：按用户时区的日期统计提交的正式任务（排除试跑，成功失败都算）。
// year 为 0 时是最近一年 [今天-364, 今天]；指定年份时是该自然年（今年截到今天）。days 只含 count>0 的日子。
func (s *MeService) Activity(ctx context.Context, userID uint64, req *model.MeActivityReq) (*model.MeActivityView, error) {
	// 1. 时区：非法回落上海，响应里回填实际使用的时区，前端据此补齐日期
	loc := activityLocation(req.TZ)

	// 2. 可选年份：注册年份到今年（按用户时区算）
	u, err := s.loadUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	today := dateOf(s.Now(), loc)
	thisYear := today.Year()
	regYear := min(u.CreatedAt.In(loc).Year(), thisYear) // 时钟偏差导致注册时间在“未来”时，至少保留今年
	years := make([]int, 0, thisYear-regYear+1)
	for y := regYear; y <= thisYear; y++ {
		years = append(years, y)
	}

	// 3. 区间（闭区间，按用户时区的日期）：没传年份是最近一年；传了年份必须在可选范围内，否则 10001
	start, end := today.AddDate(0, 0, -364), today
	if req.Year != 0 {
		if req.Year < regYear || req.Year > thisYear {
			return nil, errcode.ErrInvalidParams.WithMsg("年份超出范围")
		}
		start = time.Date(req.Year, 1, 1, 0, 0, 0, 0, loc)
		end = time.Date(req.Year, 12, 31, 0, 0, 0, 0, loc)
		if end.After(today) {
			end = today // 今年只统计到今天
		}
	}

	// 4. 查询：[start 0 点, end 次日 0 点) 换成绝对时间交给库，库里再按同一时区分桶；只查本人
	days, err := s.Repo.ActivityDays(ctx, userID, loc.String(), start, end.AddDate(0, 0, 1))
	if err != nil {
		return nil, err
	}
	if days == nil {
		days = []model.ActivityDay{}
	}
	total := 0
	for _, d := range days {
		total += d.Count
	}
	return &model.MeActivityView{
		TZ: loc.String(), Start: start.Format(activityDateLayout), End: end.Format(activityDateLayout),
		Years: years, Total: total, Days: days,
	}, nil
}
