package web_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/luvsagiri/nocturnedesk/internal/auth"
	"github.com/luvsagiri/nocturnedesk/internal/project"
)

func TestSlice5Calendar(t *testing.T) {
	srv, _, database := setupTestServer(t)
	defer database.Close()

	// 注册用户 A 和 用户 B
	userA, _ := auth.Register(database, "cal_user_a", "爱丽丝", "passA")
	tokenA := auth.CreateSessionToken(userA.ID, 3600*1e9, srv.Config().SecretKey)
	cookieA := &http.Cookie{Name: auth.SessionCookieName, Value: tokenA}

	userB, _ := auth.Register(database, "cal_user_b", "鲍勃", "passB")
	tokenB := auth.CreateSessionToken(userB.ID, 3600*1e9, srv.Config().SecretKey)
	cookieB := &http.Cookie{Name: auth.SessionCookieName, Value: tokenB}

	// 用户 A 创建项目
	proj, err := project.CreateProject(database, userA.ID, "日历测试项目", "测试每人每天纯文本日历", "#6366f1")
	if err != nil {
		t.Fatalf("Failed to create project: %v", err)
	}

	// 用户 B 加入项目
	_, err = project.JoinProjectByInviteCode(database, userB.ID, proj.InviteCode)
	if err != nil {
		t.Fatalf("Failed for user B to join project: %v", err)
	}

	testDay := "2026-10-15"
	testMonth := "2026-10"

	// 1. 验收：用户 A 提交当天记录
	form1 := url.Values{
		"day":     {testDay},
		"month":   {testMonth},
		"content": {"今天完成了登录页面开发"},
	}
	req := httptest.NewRequest("POST", fmt.Sprintf("/projects/%d/calendar", proj.ID), strings.NewReader(form1.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookieA)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusSeeOther {
		t.Fatalf("Expected 303 redirect on calendar post, got %d", w.Result().StatusCode)
	}

	var count int
	_ = database.QueryRow("SELECT COUNT(*) FROM calendar_entries WHERE project_id = ? AND user_id = ? AND day = ?", proj.ID, userA.ID, testDay).Scan(&count)
	if count != 1 {
		t.Fatalf("Expected 1 entry, got %d", count)
	}

	// 验收：同一人同一天重复提交只更新不新增（唯一约束）
	form2 := url.Values{
		"day":     {testDay},
		"month":   {testMonth},
		"content": {"今天完成了登录页面开发和数据库迁移"},
	}
	req = httptest.NewRequest("POST", fmt.Sprintf("/projects/%d/calendar", proj.ID), strings.NewReader(form2.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookieA)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusSeeOther {
		t.Fatalf("Expected 303 redirect on repeated calendar post, got %d", w.Result().StatusCode)
	}

	_ = database.QueryRow("SELECT COUNT(*) FROM calendar_entries WHERE project_id = ? AND user_id = ? AND day = ?", proj.ID, userA.ID, testDay).Scan(&count)
	if count != 1 {
		t.Fatalf("Expected strictly 1 entry due to unique constraint, got %d", count)
	}

	var updatedContent string
	_ = database.QueryRow("SELECT content FROM calendar_entries WHERE project_id = ? AND user_id = ? AND day = ?", proj.ID, userA.ID, testDay).Scan(&updatedContent)
	if updatedContent != "今天完成了登录页面开发和数据库迁移" {
		t.Fatalf("Content was not updated properly, got '%s'", updatedContent)
	}

	// 2. 用户 B 提交同一天的记录
	formB := url.Values{
		"day":     {testDay},
		"month":   {testMonth},
		"content": {"今天完成了日历组件视觉设计与调色板定义"},
	}
	req = httptest.NewRequest("POST", fmt.Sprintf("/projects/%d/calendar", proj.ID), strings.NewReader(formB.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookieB)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusSeeOther {
		t.Fatalf("Expected 303 for user B post, got %d", w.Result().StatusCode)
	}

	// 3. 验收：能看到成员名与项目色
	req = httptest.NewRequest("GET", fmt.Sprintf("/projects/%d/calendar?month=%s&day=%s", proj.ID, testMonth, testDay), nil)
	req.AddCookie(cookieA)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 on calendar get, got %d", resp.StatusCode)
	}
	body := w.Body.String()
	if !strings.Contains(body, "爱丽丝") || !strings.Contains(body, "鲍勃") {
		t.Fatalf("Expected calendar page to show both member names, got body: %s", body)
	}
	if !strings.Contains(body, "今天完成了登录页面开发和数据库迁移") || !strings.Contains(body, "今天完成了日历组件视觉设计与调色板定义") {
		t.Fatalf("Expected calendar page to show both members' entries")
	}

	// 4. 验收：空日期显示为空状态
	emptyDay := "2026-10-01"
	req = httptest.NewRequest("GET", fmt.Sprintf("/projects/%d/calendar?month=%s&day=%s", proj.ID, testMonth, emptyDay), nil)
	req.AddCookie(cookieA)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 on empty day, got %d", w.Result().StatusCode)
	}
	emptyBody := w.Body.String()
	if !strings.Contains(emptyBody, "当天暂无任何成员提交记录") {
		t.Fatalf("Expected empty state message '当天暂无任何成员提交记录'")
	}
}
