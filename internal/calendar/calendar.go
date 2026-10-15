package calendar

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/luvsagiri/nocturnedesk/internal/db"
)

var (
	ErrInvalidDay = errors.New("日期格式错误，必须为 YYYY-MM-DD")
)

type Entry struct {
	ID          int64  `json:"id"`
	ProjectID   int64  `json:"project_id"`
	UserID      int64  `json:"user_id"`
	UserName    string `json:"user_name"`
	UserColor   string `json:"user_color"`
	Day         string `json:"day"`
	Content     string `json:"content"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type Contributor struct {
	UserName string `json:"user_name"`
	Color    string `json:"color"`
}

type CalendarDay struct {
	DayNumber      int           `json:"day_number"`
	DateString     string        `json:"date_string"` // YYYY-MM-DD
	IsCurrentMonth bool          `json:"is_current_month"`
	IsToday        bool          `json:"is_today"`
	Entries        []Entry       `json:"entries"`
	Contributors   []Contributor `json:"contributors"`
}

type MonthView struct {
	YearMonth    string        `json:"year_month"` // YYYY-MM
	Year         int           `json:"year"`
	Month        int           `json:"month"`
	PrevMonth    string        `json:"prev_month"`
	NextMonth    string        `json:"next_month"`
	Days         []CalendarDay `json:"days"`
	SelectedDay  string        `json:"selected_day"`
	DayEntries   []Entry       `json:"day_entries"`
	MyDayContent string        `json:"my_day_content"`
}

func UpsertEntry(database *db.DB, projectID, userID int64, day, content string) error {
	day = strings.TrimSpace(day)
	if _, err := time.Parse("2006-01-02", day); err != nil {
		return ErrInvalidDay
	}
	content = strings.TrimSpace(content)

	if content == "" {
		// 如果内容被清空，则删除当天的记录
		_, err := database.Exec(`
			DELETE FROM calendar_entries
			WHERE project_id = ? AND user_id = ? AND day = ?
		`, projectID, userID, day)
		_, _ = database.Exec(`
			DELETE FROM search_index
			WHERE project_id = ? AND kind = 'calendar' AND title LIKE ?
		`, projectID, day+"%")
		return err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	res, err := database.Exec(`
		INSERT INTO calendar_entries (project_id, user_id, day, content, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(project_id, user_id, day) DO UPDATE SET
			content = excluded.content,
			updated_at = excluded.updated_at
	`, projectID, userID, day, content, now, now)
	if err != nil {
		return fmt.Errorf("failed to upsert calendar entry: %w", err)
	}

	// 同步 search_index
	var entryID int64
	err = database.QueryRow(`
		SELECT id FROM calendar_entries WHERE project_id = ? AND user_id = ? AND day = ?
	`, projectID, userID, day).Scan(&entryID)
	if err == nil {
		_, _ = database.Exec(`DELETE FROM search_index WHERE kind = 'calendar' AND ref_id = ?`, entryID)
		_, _ = database.Exec(`
			INSERT INTO search_index (title, body, project_id, kind, ref_id)
			VALUES (?, ?, ?, 'calendar', ?)
		`, day+" 日历记录", content, projectID, entryID)
	}
	_ = res

	return nil
}

func GetEntriesForMonth(database *db.DB, projectID int64, yearMonth string) ([]Entry, error) {
	likePattern := yearMonth + "%"
	rows, err := database.Query(`
		SELECT ce.id, ce.project_id, ce.user_id, u.display_name, pm.color, ce.day, ce.content, ce.created_at, ce.updated_at
		FROM calendar_entries ce
		JOIN users u ON ce.user_id = u.id
		JOIN project_members pm ON (ce.project_id = pm.project_id AND ce.user_id = pm.user_id)
		WHERE ce.project_id = ? AND ce.day LIKE ?
		ORDER BY ce.day ASC, ce.created_at ASC
	`, projectID, likePattern)
	if err != nil {
		return nil, fmt.Errorf("query calendar entries failed: %w", err)
	}
	defer rows.Close()

	var entries []Entry
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.ID, &e.ProjectID, &e.UserID, &e.UserName, &e.UserColor, &e.Day, &e.Content, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, nil
}

func BuildMonthView(database *db.DB, projectID, currentUserID int64, yearMonth, selectedDay string) (*MonthView, error) {
	var targetTime time.Time
	var err error

	if yearMonth == "" {
		targetTime = time.Now()
		yearMonth = targetTime.Format("2006-01")
	} else {
		targetTime, err = time.Parse("2006-01", yearMonth)
		if err != nil {
			targetTime = time.Now()
			yearMonth = targetTime.Format("2006-01")
		}
	}

	year, month, _ := targetTime.Date()
	todayStr := time.Now().Format("2006-01-02")
	if selectedDay == "" {
		if yearMonth == todayStr[:7] {
			selectedDay = todayStr
		} else {
			selectedDay = fmt.Sprintf("%04d-%02d-01", year, month)
		}
	}

	prevTime := targetTime.AddDate(0, -1, 0)
	nextTime := targetTime.AddDate(0, 1, 0)

	entries, err := GetEntriesForMonth(database, projectID, yearMonth)
	if err != nil {
		return nil, err
	}

	// 映射每组日期的记录
	entriesByDay := make(map[string][]Entry)
	for _, e := range entries {
		entriesByDay[e.Day] = append(entriesByDay[e.Day], e)
	}

	// 构建当月网格 (从周一开始)
	firstOfMonth := time.Date(year, month, 1, 0, 0, 0, 0, time.Local)
	lastOfMonth := firstOfMonth.AddDate(0, 1, -1)

	// 计算第一天是周几 (周一为 1 ... 周日为 7)
	firstWeekday := int(firstOfMonth.Weekday())
	if firstWeekday == 0 {
		firstWeekday = 7
	}

	// 前置补齐天数
	daysBefore := firstWeekday - 1
	gridStart := firstOfMonth.AddDate(0, 0, -daysBefore)

	// 网格生成 42 天 (6 行 7 列，固定高度整齐美观)
	var calendarDays []CalendarDay
	for i := 0; i < 42; i++ {
		curDate := gridStart.AddDate(0, 0, i)
		dateStr := curDate.Format("2006-01-02")
		isCurMonth := curDate.Month() == month
		isToday := dateStr == todayStr

		dayEntries := entriesByDay[dateStr]
		var contributors []Contributor
		seen := make(map[string]bool)
		for _, e := range dayEntries {
			if !seen[e.UserName] {
				seen[e.UserName] = true
				contributors = append(contributors, Contributor{
					UserName: e.UserName,
					Color:    e.UserColor,
				})
			}
		}

		calendarDays = append(calendarDays, CalendarDay{
			DayNumber:      curDate.Day(),
			DateString:     dateStr,
			IsCurrentMonth: isCurMonth,
			IsToday:        isToday,
			Entries:        dayEntries,
			Contributors:   contributors,
		})

		// 如果已经到当前月末且恰好在这一周结束，可以不用铺满42天，但保证至少35天
		if i >= 34 && curDate.After(lastOfMonth) && curDate.Weekday() == time.Sunday {
			break
		}
	}

	// 获取选中日期的全员记录
	dayEntries := entriesByDay[selectedDay]
	if len(dayEntries) == 0 {
		// 如果 selectedDay 不在当月记录映射内，从数据库精确查一次
		rows, err := database.Query(`
			SELECT ce.id, ce.project_id, ce.user_id, u.display_name, pm.color, ce.day, ce.content, ce.created_at, ce.updated_at
			FROM calendar_entries ce
			JOIN users u ON ce.user_id = u.id
			JOIN project_members pm ON (ce.project_id = pm.project_id AND ce.user_id = pm.user_id)
			WHERE ce.project_id = ? AND ce.day = ?
			ORDER BY ce.created_at ASC
		`, projectID, selectedDay)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var e Entry
				if err := rows.Scan(&e.ID, &e.ProjectID, &e.UserID, &e.UserName, &e.UserColor, &e.Day, &e.Content, &e.CreatedAt, &e.UpdatedAt); err == nil {
					dayEntries = append(dayEntries, e)
				}
			}
		}
	}

	var myDayContent string
	for _, e := range dayEntries {
		if e.UserID == currentUserID {
			myDayContent = e.Content
			break
		}
	}

	return &MonthView{
		YearMonth:    yearMonth,
		Year:         year,
		Month:        int(month),
		PrevMonth:    prevTime.Format("2006-01"),
		NextMonth:    nextTime.Format("2006-01"),
		Days:         calendarDays,
		SelectedDay:  selectedDay,
		DayEntries:   dayEntries,
		MyDayContent: myDayContent,
	}, nil
}
