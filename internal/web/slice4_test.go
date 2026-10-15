package web_test

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luvsagiri/nocturnedesk/internal/auth"
	"github.com/luvsagiri/nocturnedesk/internal/item"
	"github.com/luvsagiri/nocturnedesk/internal/project"
	"github.com/luvsagiri/nocturnedesk/internal/web"
)

func TestSlice4FoldersDocsAndFiles(t *testing.T) {
	srv, _, database := setupTestServer(t)
	defer database.Close()

	// 注册用户 A 和 用户 B
	userA, _ := auth.Register(database, "slice4_user_a", "用户A", "passwordA")
	tokenA := auth.CreateSessionToken(userA.ID, 3600*1e9, srv.Config().SecretKey)
	cookieA := &http.Cookie{Name: auth.SessionCookieName, Value: tokenA}

	userB, _ := auth.Register(database, "slice4_user_b", "用户B", "passwordB")
	tokenB := auth.CreateSessionToken(userB.ID, 3600*1e9, srv.Config().SecretKey)
	cookieB := &http.Cookie{Name: auth.SessionCookieName, Value: tokenB}

	// 用户 A 创建项目
	proj, err := project.CreateProject(database, userA.ID, "切片4测试项目", "文档与资源测试", "#6366f1")
	if err != nil {
		t.Fatalf("Failed to create project: %v", err)
	}

	// 1. 文件夹树：创建根文件夹 Folder1、Folder2
	formF1 := url.Values{"name": {"设计素材与规范"}}
	req := httptest.NewRequest("POST", fmt.Sprintf("/projects/%d/folders", proj.ID), strings.NewReader(formF1.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookieA)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusSeeOther {
		t.Fatalf("Expected 303 on folder 1 creation, got %d", w.Result().StatusCode)
	}

	formF2 := url.Values{"name": {"后端架构与规范"}}
	req = httptest.NewRequest("POST", fmt.Sprintf("/projects/%d/folders", proj.ID), strings.NewReader(formF2.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookieA)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusSeeOther {
		t.Fatalf("Expected 303 on folder 2 creation, got %d", w.Result().StatusCode)
	}

	folders, err := item.ListFoldersInProject(database, proj.ID)
	if err != nil || len(folders) != 2 {
		t.Fatalf("Expected 2 folders, got %d, err=%v", len(folders), err)
	}
	f1 := folders[0]
	f2 := folders[1]

	// 2. 验收：移动文件夹后树结构正确
	// 将 f2 移动到 f1 下成为子文件夹
	formMove := url.Values{"target_parent_id": {fmt.Sprintf("%d", f1.ID)}}
	req = httptest.NewRequest("POST", fmt.Sprintf("/folders/%d/move", f2.ID), strings.NewReader(formMove.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookieA)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusSeeOther {
		t.Fatalf("Expected 303 on folder move, got %d", w.Result().StatusCode)
	}

	// 验证 f2 的 parent_id 确实变为 f1.ID
	f2Reloaded, err := item.GetFolder(database, f2.ID)
	if err != nil || f2Reloaded.ParentID == nil || *f2Reloaded.ParentID != f1.ID {
		t.Fatalf("Expected f2.ParentID to be %d, got %v", f1.ID, f2Reloaded.ParentID)
	}

	// 验证根目录下现在只有 1 个子文件夹 (f1)
	rootSubfolders, err := item.ListSubfolders(database, proj.ID, nil)
	if err != nil || len(rootSubfolders) != 1 || rootSubfolders[0].ID != f1.ID {
		t.Fatalf("Expected only f1 under root, got %d folders", len(rootSubfolders))
	}

	// 验证 f1 下有 1 个子文件夹 (f2)
	f1Subfolders, err := item.ListSubfolders(database, proj.ID, &f1.ID)
	if err != nil || len(f1Subfolders) != 1 || f1Subfolders[0].ID != f2.ID {
		t.Fatalf("Expected f2 under f1, got %d folders", len(f1Subfolders))
	}

	// 3. 验收：含中文的资源名与文档标题正常显示
	// 在 f2 下创建富文本文档（中文标题 + 中文正文）
	docForm := url.Values{
		"title":     {"核心架构设计与数据库迁移指南"},
		"body_html": {"<p>本项目采用 <strong>SQLite + WAL</strong> 单二进制架构，离线可运行。</p>"},
		"folder_id": {fmt.Sprintf("%d", f2.ID)},
	}
	req = httptest.NewRequest("POST", fmt.Sprintf("/projects/%d/docs", proj.ID), strings.NewReader(docForm.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookieA)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	resp := w.Result()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("Expected 303 on doc creation, got %d", resp.StatusCode)
	}
	docLoc := resp.Header.Get("Location")
	if !strings.Contains(docLoc, "/docs/") {
		t.Fatalf("Expected redirect to /docs/{iid}, got %s", docLoc)
	}

	// 访问文档阅读页，检查中文显示正常
	req = httptest.NewRequest("GET", docLoc, nil)
	req.AddCookie(cookieA)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	docPage := w.Body.String()
	if !strings.Contains(docPage, "核心架构设计与数据库迁移指南") || !strings.Contains(docPage, "SQLite + WAL") {
		t.Fatalf("Expected doc view to display Chinese title and content correctly")
	}

	// 上传带有中文文件名的资源文件
	chineseFileName := "需求定稿说明书_v2.0.pdf"
	fakePDFData := []byte("%PDF-1.4 header and sample pdf content")
	bodyBuf := &bytes.Buffer{}
	mpWriter := multipart.NewWriter(bodyBuf)
	_ = mpWriter.WriteField("folder_id", fmt.Sprintf("%d", f2.ID))
	part, _ := mpWriter.CreateFormFile("file", chineseFileName)
	_, _ = part.Write(fakePDFData)
	mpWriter.Close()

	req = httptest.NewRequest("POST", fmt.Sprintf("/projects/%d/upload", proj.ID), bodyBuf)
	req.Header.Set("Content-Type", mpWriter.FormDataContentType())
	req.AddCookie(cookieA)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusSeeOther {
		t.Fatalf("Expected 303 on file upload, got %d", w.Result().StatusCode)
	}

	// 在文件列表中检查中文资源文件名
	req = httptest.NewRequest("GET", fmt.Sprintf("/projects/%d/files?folder=%d", proj.ID, f2.ID), nil)
	req.AddCookie(cookieA)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	filesPage := w.Body.String()
	if !strings.Contains(filesPage, chineseFileName) {
		t.Fatalf("Expected files list to contain Chinese file name '%s'", chineseFileName)
	}

	// 4. 验收：非成员拿不到文件字节流
	itemsInF2, err := item.ListItems(database, proj.ID, &f2.ID, "file")
	if err != nil || len(itemsInF2) == 0 {
		t.Fatalf("Expected to find uploaded file in items")
	}
	fileItem := itemsInF2[0]

	// 用户 A (成员) 下载文件成功
	req = httptest.NewRequest("GET", fmt.Sprintf("/files/%d", fileItem.ID), nil)
	req.AddCookie(cookieA)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("Expected member A to download file with 200, got %d", w.Result().StatusCode)
	}
	downloadedBytes, _ := io.ReadAll(w.Body)
	if !bytes.Equal(downloadedBytes, fakePDFData) {
		t.Fatalf("Downloaded bytes do not match uploaded data")
	}

	// 用户 B (非成员) 请求 /files/{iid} → 404 (不是 200，拿不到字节流)
	req = httptest.NewRequest("GET", fmt.Sprintf("/files/%d", fileItem.ID), nil)
	req.AddCookie(cookieB)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusNotFound {
		t.Fatalf("Expected non-member B to get 404 on /files/{iid}, got %d", w.Result().StatusCode)
	}

	// 5. 验收：上传 201MB 文件被拒并给明确提示
	// 创建一个小的测试 Server，将 MaxUploadMB 设为 1MB (模拟上限机制，超出的文件被拒且给出明确提示)
	smallCfg := *srv.Config()
	smallCfg.MaxUploadMB = 1
	smallSrv := web.NewServer(&smallCfg, database, os.DirFS(filepath.Join("..", "..")), os.DirFS(filepath.Join("..", "..")))

	bigBuf := &bytes.Buffer{}
	bigWriter := multipart.NewWriter(bigBuf)
	bigPart, _ := bigWriter.CreateFormFile("file", "oversized_file.dat")
	// 写入 1.5MB 数据（超过 1MB 限制）
	oversizedBytes := make([]byte, 1500*1024)
	_, _ = bigPart.Write(oversizedBytes)
	bigWriter.Close()

	req = httptest.NewRequest("POST", fmt.Sprintf("/projects/%d/upload", proj.ID), bigBuf)
	req.Header.Set("Content-Type", bigWriter.FormDataContentType())
	req.AddCookie(cookieA)
	w = httptest.NewRecorder()
	smallSrv.ServeHTTP(w, req)
	resp = w.Result()
	redirectLoc := resp.Header.Get("Location")
	if !strings.Contains(redirectLoc, "error=") {
		t.Fatalf("Expected oversized upload to redirect with error, got: %s", redirectLoc)
	}
	// 验证有明确的超限错误提示
	decodedLoc, _ := url.QueryUnescape(redirectLoc)
	if !strings.Contains(decodedLoc, "文件大小超过上限") {
		t.Fatalf("Expected clear error message '文件大小超过上限', got: %s", decodedLoc)
	}
}
