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

func TestSlice3ProjectAndVisibility(t *testing.T) {
	srv, _, database := setupTestServer(t)
	defer database.Close()

	// 注册用户 A (Alice)
	userA, err := auth.Register(database, "user_a", "爱丽丝", "passA")
	if err != nil {
		t.Fatalf("Failed to register user A: %v", err)
	}
	tokenA := auth.CreateSessionToken(userA.ID, 3600*1e9, srv.Config().SecretKey)
	cookieA := &http.Cookie{Name: auth.SessionCookieName, Value: tokenA}

	// 注册用户 B (Bob)
	userB, err := auth.Register(database, "user_b", "鲍勃", "passB")
	if err != nil {
		t.Fatalf("Failed to register user B: %v", err)
	}
	tokenB := auth.CreateSessionToken(userB.ID, 3600*1e9, srv.Config().SecretKey)
	cookieB := &http.Cookie{Name: auth.SessionCookieName, Value: tokenB}

	// 1. 用户 A 创建项目 ProjA
	projA, err := project.CreateProject(database, userA.ID, "Alice Secret Proj", "A 的绝密项目", "#6366f1")
	if err != nil {
		t.Fatalf("Failed to create project A: %v", err)
	}
	_ = projA

	// 2. 用户 B 创建项目 ProjB
	projB, err := project.CreateProject(database, userB.ID, "Bob Finance Proj", "B 的财务项目", "#10b981")
	if err != nil {
		t.Fatalf("Failed to create project B: %v", err)
	}

	// 3. 验收：用户 A 的首页看不到用户 B 的项目
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(cookieA)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 on home for user A, got %d", resp.StatusCode)
	}
	bodyA := w.Body.String()
	if !strings.Contains(bodyA, "Alice Secret Proj") {
		t.Fatalf("Expected User A to see own project ProjA")
	}
	if strings.Contains(bodyA, "Bob Finance Proj") {
		t.Fatalf("User A must NOT see User B's project ProjB!")
	}

	// 4. 验收：非成员直接请求 /projects/{pid}/files → 404
	req = httptest.NewRequest("GET", fmt.Sprintf("/projects/%d/files", projB.ID), nil)
	req.AddCookie(cookieA)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusNotFound {
		t.Fatalf("Expected non-member accessing /projects/{pid}/files to get 404, got %d", w.Result().StatusCode)
	}

	// 5. 验收：非成员直接请求 /files/{iid} → 404
	// 在 ProjB 插入一条文件记录
	res, err := database.Exec(`
		INSERT INTO items (project_id, folder_id, kind, title, storage_key, original_name, mime, size_bytes, created_by, created_at, updated_at)
		VALUES (?, NULL, 'file', 'secret.pdf', 'uploads/test.pdf', 'secret.pdf', 'application/pdf', 1024, ?, '2026-10-15T00:00:00Z', '2026-10-15T00:00:00Z')
	`, projB.ID, userB.ID)
	if err != nil {
		t.Fatalf("Failed to insert item into ProjB: %v", err)
	}
	fileIID, _ := res.LastInsertId()

	req = httptest.NewRequest("GET", fmt.Sprintf("/files/%d", fileIID), nil)
	req.AddCookie(cookieA)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusNotFound {
		t.Fatalf("Expected non-member accessing /files/{iid} to get 404, got %d", w.Result().StatusCode)
	}

	// 6. 验收：非成员直接请求 /search?q=（指定非成员项目或在非成员项目内搜索）→ 404
	req = httptest.NewRequest("GET", fmt.Sprintf("/search?q=secret&pid=%d", projB.ID), nil)
	req.AddCookie(cookieA)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusNotFound {
		t.Fatalf("Expected non-member querying /search with pid={projB} to get 404, got %d", w.Result().StatusCode)
	}

	req = httptest.NewRequest("GET", fmt.Sprintf("/projects/%d/search?q=secret", projB.ID), nil)
	req.AddCookie(cookieA)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusNotFound {
		t.Fatalf("Expected non-member querying /projects/{pid}/search to get 404, got %d", w.Result().StatusCode)
	}

	// 7. 验收：重置邀请码后旧码加入失败
	oldInviteCode := projB.InviteCode

	// 用户 B 重置邀请码
	req = httptest.NewRequest("POST", fmt.Sprintf("/projects/%d/invite/reset", projB.ID), nil)
	req.AddCookie(cookieB)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusSeeOther {
		t.Fatalf("Expected 303 redirect on invite reset, got %d", w.Result().StatusCode)
	}

	// 用户 A 用旧邀请码加入 ProjB → 失败
	joinForm := url.Values{"invite_code": {oldInviteCode}}
	req = httptest.NewRequest("POST", "/projects/join", strings.NewReader(joinForm.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookieA)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	resp = w.Result()
	// 重定向回 / 并且带有错误参数
	redirectLoc := resp.Header.Get("Location")
	if !strings.Contains(redirectLoc, "error=") {
		t.Fatalf("Expected join with old invite code to fail, got redirect to: %s", redirectLoc)
	}

	// 获取新的邀请码
	var newInviteCode string
	err = database.QueryRow("SELECT invite_code FROM projects WHERE id = ?", projB.ID).Scan(&newInviteCode)
	if err != nil || newInviteCode == oldInviteCode {
		t.Fatalf("Invite code was not refreshed properly")
	}

	// 用户 A 用新邀请码加入 ProjB → 成功
	newJoinForm := url.Values{"invite_code": {newInviteCode}}
	req = httptest.NewRequest("POST", "/projects/join", strings.NewReader(newJoinForm.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookieA)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	resp = w.Result()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("Expected 303 redirect on successful join, got %d", resp.StatusCode)
	}

	// 用户 A 现在成为 ProjB 成员，可以访问 /projects/{pid}/files
	req = httptest.NewRequest("GET", fmt.Sprintf("/projects/%d/files", projB.ID), nil)
	req.AddCookie(cookieA)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("Expected member A to access /projects/{pid}/files with 200, got %d, body: %s", w.Result().StatusCode, w.Body.String())
	}

	// 8. 验收：owner 调 /leave 被拒
	req = httptest.NewRequest("POST", fmt.Sprintf("/projects/%d/leave", projB.ID), nil)
	req.AddCookie(cookieB) // Bob is owner
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	resp = w.Result()
	redirectLoc = resp.Header.Get("Location")
	if !strings.Contains(redirectLoc, "error=") {
		t.Fatalf("Expected owner leave to fail with error redirect, got %s", redirectLoc)
	}

	// 验证 Bob 依然是 owner
	_, memberBob, err := project.GetProjectDetails(database, projB.ID, userB.ID)
	if err != nil || memberBob.Role != "owner" {
		t.Fatalf("Bob should still be owner of ProjB")
	}

	// 用户 A (member) 调 /leave 可以退出成功
	req = httptest.NewRequest("POST", fmt.Sprintf("/projects/%d/leave", projB.ID), nil)
	req.AddCookie(cookieA)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	resp = w.Result()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("Expected 303 redirect on member leave, got %d", resp.StatusCode)
	}

	// 退出后用户 A 再次访问 ProjB → 恢复 404
	req = httptest.NewRequest("GET", fmt.Sprintf("/projects/%d/files", projB.ID), nil)
	req.AddCookie(cookieA)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusNotFound {
		t.Fatalf("Expected ex-member A accessing /projects/{pid}/files to get 404, got %d", w.Result().StatusCode)
	}
}
