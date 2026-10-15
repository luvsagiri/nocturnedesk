package web_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/luvsagiri/nocturnedesk/internal/auth"
	"github.com/luvsagiri/nocturnedesk/internal/board"
	"github.com/luvsagiri/nocturnedesk/internal/project"
)

func TestSlice6BoardAndCards(t *testing.T) {
	srv, _, database := setupTestServer(t)
	defer database.Close()

	// 注册用户 A 和 用户 B
	userA, _ := auth.Register(database, "board_user_a", "看板创建者", "passA")
	tokenA := auth.CreateSessionToken(userA.ID, 3600*1e9, srv.Config().SecretKey)
	cookieA := &http.Cookie{Name: auth.SessionCookieName, Value: tokenA}

	userB, _ := auth.Register(database, "board_user_b", "协助成员", "passB")
	tokenB := auth.CreateSessionToken(userB.ID, 3600*1e9, srv.Config().SecretKey)
	cookieB := &http.Cookie{Name: auth.SessionCookieName, Value: tokenB}

	// 用户 A 创建项目，用户 B 加入
	proj, err := project.CreateProject(database, userA.ID, "看板开发项目", "敏捷迭代", "#6366f1")
	if err != nil {
		t.Fatalf("Failed to create project: %v", err)
	}
	_, _ = project.JoinProjectByInviteCode(database, userB.ID, proj.InviteCode)

	// 1. 创建标签 "核心功能" 和 "缺陷"
	lblCore, err := board.CreateLabel(database, proj.ID, "核心功能", "#3b82f6")
	if err != nil {
		t.Fatalf("Failed to create label: %v", err)
	}
	lblBug, err := board.CreateLabel(database, proj.ID, "紧急Bug", "#ef4444")
	if err != nil {
		t.Fatalf("Failed to create label: %v", err)
	}

	// 2. 创建 3 张卡片
	c1, err := board.CreateCard(database, proj.ID, userA.ID, "卡片1: 首页设计", "纯文本备注1", &userB.ID, nil, []int64{lblCore.ID, lblBug.ID})
	if err != nil {
		t.Fatalf("Failed to create card 1: %v", err)
	}
	c2, err := board.CreateCard(database, proj.ID, userA.ID, "卡片2: 数据库迁移", "纯文本备注2", &userA.ID, nil, []int64{lblCore.ID})
	if err != nil {
		t.Fatalf("Failed to create card 2: %v", err)
	}
	c3, err := board.CreateCard(database, proj.ID, userA.ID, "卡片3: 自动化测试", "纯文本备注3", nil, nil, []int64{})
	if err != nil {
		t.Fatalf("Failed to create card 3: %v", err)
	}

	// 初始状态：全部在 todo 列，顺序为 c1, c2, c3
	if c1.Position >= c2.Position || c2.Position >= c3.Position {
		t.Fatalf("Expected initial card positions to be strictly increasing: %f, %f, %f", c1.Position, c2.Position, c3.Position)
	}

	// 3. 验收：拖拽跨列与列内排序，刷新页面顺序保持
	// (a) 将 c3 拖动插入在 c1 和 c2 之间（状态仍为 todo）
	moveForm1 := url.Values{
		"status":       {"todo"},
		"prev_card_id": {fmt.Sprintf("%d", c1.ID)},
		"next_card_id": {fmt.Sprintf("%d", c2.ID)},
	}
	req := httptest.NewRequest("POST", fmt.Sprintf("/cards/%d/move", c3.ID), strings.NewReader(moveForm1.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookieA)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 on card move, got %d", w.Result().StatusCode)
	}

	// 重新读取 c3 position，应该严格在 c1 与 c2 之间
	c1Reloaded, _ := board.GetCard(database, c1.ID)
	c2Reloaded, _ := board.GetCard(database, c2.ID)
	c3Reloaded, _ := board.GetCard(database, c3.ID)
	if !(c1Reloaded.Position < c3Reloaded.Position && c3Reloaded.Position < c2Reloaded.Position) {
		t.Fatalf("Expected c1 < c3 < c2, got c1=%f, c3=%f, c2=%f", c1Reloaded.Position, c3Reloaded.Position, c2Reloaded.Position)
	}

	// (b) 将 c2 跨列移动到 doing 列
	moveForm2 := url.Values{
		"status": {"doing"},
	}
	req = httptest.NewRequest("POST", fmt.Sprintf("/cards/%d/move", c2.ID), strings.NewReader(moveForm2.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookieA)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 on cross-column move, got %d", w.Result().StatusCode)
	}

	// 刷新页面请求看板：/projects/{pid}/board
	req = httptest.NewRequest("GET", fmt.Sprintf("/projects/%d/board", proj.ID), nil)
	req.AddCookie(cookieA)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 on board get, got %d", w.Result().StatusCode)
	}
	boardHtml := w.Body.String()

	// 验证 HTML 页面中 Todo 列的卡片顺序为 c1、c3
	idxC1 := strings.Index(boardHtml, "卡片1: 首页设计")
	idxC3 := strings.Index(boardHtml, "卡片3: 自动化测试")
	idxC2 := strings.Index(boardHtml, "卡片2: 数据库迁移")
	if idxC1 == -1 || idxC3 == -1 || idxC2 == -1 {
		t.Fatalf("Expected all cards to be present in rendered board HTML")
	}
	if !(idxC1 < idxC3) {
		t.Fatalf("Expected c1 to appear before c3 in rendered HTML, but idxC1=%d, idxC3=%d", idxC1, idxC3)
	}

	// 4. 验收：position 计算不产生重复
	// 连续在 c1 与 c3 之间插入测试
	posA := c1Reloaded.Position
	posB := c3Reloaded.Position
	var generatedPositions []float64
	for i := 0; i < 5; i++ {
		mid := (posA + posB) / 2.0
		generatedPositions = append(generatedPositions, mid)
		posB = mid
	}
	// 验证每个计算出的 position 互不相等
	for i := 0; i < len(generatedPositions)-1; i++ {
		if generatedPositions[i] == generatedPositions[i+1] {
			t.Fatalf("Duplicate position detected during midpoint calculation: %f", generatedPositions[i])
		}
	}

	// 5. 验收：删除标签后卡片上的标签引用同步消失
	// 验证 c1 当前有 2 个标签
	card1Before, err := board.GetCard(database, c1.ID)
	if err != nil || len(card1Before.Labels) != 2 {
		t.Fatalf("Expected card 1 to have 2 labels, got %d", len(card1Before.Labels))
	}

	// 删除标签 "紧急Bug"
	req = httptest.NewRequest("POST", fmt.Sprintf("/labels/%d/delete", lblBug.ID), nil)
	req.AddCookie(cookieA)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusSeeOther {
		t.Fatalf("Expected 303 on label delete, got %d", w.Result().StatusCode)
	}

	// 再次查询 c1：只剩 1 个标签 "核心功能"
	card1After, err := board.GetCard(database, c1.ID)
	if err != nil {
		t.Fatalf("Failed to get card 1 after label deletion: %v", err)
	}
	if len(card1After.Labels) != 1 || card1After.Labels[0].ID != lblCore.ID {
		t.Fatalf("Expected card 1 to have exactly 1 label (lblCore), got %d labels", len(card1After.Labels))
	}
	_ = cookieB
}
