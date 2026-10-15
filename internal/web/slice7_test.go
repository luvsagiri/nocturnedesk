package web_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/luvsagiri/nocturnedesk/internal/auth"
	"github.com/luvsagiri/nocturnedesk/internal/board"
	"github.com/luvsagiri/nocturnedesk/internal/calendar"
	"github.com/luvsagiri/nocturnedesk/internal/item"
	"github.com/luvsagiri/nocturnedesk/internal/project"
	"github.com/luvsagiri/nocturnedesk/internal/search"
)

func TestSlice7Search(t *testing.T) {
	srv, _, database := setupTestServer(t)
	defer database.Close()

	// 注册用户 A 和 用户 B
	userA, _ := auth.Register(database, "search_user_a", "用户A", "passA")
	tokenA := auth.CreateSessionToken(userA.ID, 3600*1e9, srv.Config().SecretKey)
	cookieA := &http.Cookie{Name: auth.SessionCookieName, Value: tokenA}

	userB, _ := auth.Register(database, "search_user_b", "用户B", "passB")
	tokenB := auth.CreateSessionToken(userB.ID, 3600*1e9, srv.Config().SecretKey)
	cookieB := &http.Cookie{Name: auth.SessionCookieName, Value: tokenB}

	// 用户 A 的项目 ProjA
	projA, err := project.CreateProject(database, userA.ID, "公开研发项目", "A 的项目", "#6366f1")
	if err != nil {
		t.Fatalf("Failed to create projA: %v", err)
	}

	// 用户 B 的项目 ProjB (私密，用户 A 非成员)
	projB, err := project.CreateProject(database, userB.ID, "私密财务项目", "B 的绝密项目", "#ef4444")
	if err != nil {
		t.Fatalf("Failed to create projB: %v", err)
	}

	// 1. 在 ProjA 创建各类资源以验证 trigram 中文命中与立即可搜
	// (a) 文档
	docA, err := item.CreateDoc(database, projA.ID, nil, userA.ID, "核心架构设计与数据库迁移指南", "<p>今天讨论了安全鉴权逻辑与会话签名机制。</p>")
	if err != nil {
		t.Fatalf("Failed to create doc: %v", err)
	}
	_ = docA

	// (b) 日历
	err = calendar.UpsertEntry(database, projA.ID, userA.ID, "2026-10-15", "今天攻关了分布式事务重试方案，排查了超时异常。")
	if err != nil {
		t.Fatalf("Failed to upsert calendar entry: %v", err)
	}

	// (c) 看板卡片
	cardA, err := board.CreateCard(database, projA.ID, userA.ID, "解决高并发连接池死锁问题", "重构连接获取链路与超时上下文", nil, nil, nil)
	if err != nil {
		t.Fatalf("Failed to create card: %v", err)
	}
	_ = cardA

	// (d) 文件
	fileA, err := item.CreateFile(database, projA.ID, nil, userA.ID, "需求规格说明书2026定稿.pdf", "application/pdf", 2048, "uploads/req.pdf")
	if err != nil {
		t.Fatalf("Failed to create file item: %v", err)
	}
	_ = fileA

	// 2. 在 ProjB 创建带有私密关键词的内容
	_, err = item.CreateDoc(database, projB.ID, nil, userB.ID, "鲍勃私密股权分红方案", "<p>这是只有项目 B 成员可见的机密数据。</p>")
	if err != nil {
		t.Fatalf("Failed to create doc in ProjB: %v", err)
	}

	// 3. 验收：中文关键词可命中（trigram）且保存后立即可搜
	testKeywords := []string{
		"数据库迁移",
		"安全鉴权",
		"分布式事务",
		"连接池死锁",
		"需求规格说明书",
	}

	for _, kw := range testKeywords {
		req := httptest.NewRequest("GET", "/search?q="+url.QueryEscape(kw), nil)
		req.AddCookie(cookieA)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		resp := w.Result()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200 on search '%s', got %d", kw, resp.StatusCode)
		}
		body := w.Body.String()
		if !strings.Contains(body, kw) && !strings.Contains(body, "<b>"+kw) {
			t.Fatalf("Expected search result to contain keyword '%s', but got body:\n%s", kw, body)
		}
	}

	// 4. 验收：搜到的每一条都属于当前用户可见项目（可见性过滤）
	// 用户 A 搜索 ProjB 中的私密词 "股权分红" → 必须为 0 条结果，绝对不可见！
	req := httptest.NewRequest("GET", "/search?q="+url.QueryEscape("股权分红"), nil)
	req.AddCookie(cookieA)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	bodyA := w.Body.String()
	if strings.Contains(bodyA, "鲍勃私密股权分红方案") {
		t.Fatalf("CRITICAL SECURITY VIOLATION: Non-member user A found private project B document in search!")
	}
	if !strings.Contains(bodyA, "未在可见项目中找到") {
		t.Fatalf("Expected empty search state for non-member query, got: %s", bodyA)
	}

	// 用户 B (作为 ProjB 成员) 搜索 "股权分红" → 能搜到！
	req = httptest.NewRequest("GET", "/search?q="+url.QueryEscape("股权分红"), nil)
	req.AddCookie(cookieB)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	bodyB := w.Body.String()
	if !strings.Contains(bodyB, "鲍勃私密股权分红方案") {
		t.Fatalf("Expected member B to find own document, got body: %s", bodyB)
	}

	// 5. 验收：FTS5 退化到 LIKE 检索机制有效
	likeResults, err := search.PerformSearch(database, userA.ID, 0, "高并发连接池")
	if err != nil || len(likeResults) == 0 {
		t.Fatalf("Expected search to return results, got %v, err=%v", likeResults, err)
	}
	if !strings.Contains(likeResults[0].Title, "连接池") {
		t.Fatalf("Expected title to match connection pool card")
	}
}
