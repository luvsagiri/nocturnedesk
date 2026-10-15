package web_test

import (
	"bytes"
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
	"github.com/luvsagiri/nocturnedesk/internal/config"
	"github.com/luvsagiri/nocturnedesk/internal/db"
	"github.com/luvsagiri/nocturnedesk/internal/web"
)

func setupTestServer(t *testing.T) (*web.Server, *config.Config, *db.DB) {
	t.Helper()
	tempDir := t.TempDir()
	cfg := &config.Config{
		Port:            "8080",
		DataDir:         tempDir,
		MaxUploadMB:     200,
		SessionDuration: 30 * 24 * 3600 * 1e9,
		SecretKey:       []byte("01234567890123456789012345678901"),
	}

	database, err := db.Connect(cfg.DBPath())
	if err != nil {
		t.Fatalf("Failed to connect and migrate db: %v", err)
	}

	srv := web.NewServer(cfg, database, os.DirFS(filepath.Join("..", "..")), os.DirFS(filepath.Join("..", "..")))
	return srv, cfg, database
}

func TestSlice2AuthAndAccount(t *testing.T) {
	srv, cfg, database := setupTestServer(t)
	defer database.Close()

	// 1. 验收：未登录访问受保护页 → 302 /login
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	resp := w.Result()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("Expected 302 redirect to /login, got %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if loc != "/login" {
		t.Fatalf("Expected Location /login, got %s", loc)
	}

	// 2. 注册账号: user1
	regForm := url.Values{
		"username":     {"Alice_User"},
		"display_name": {"爱丽丝"},
		"password":     {"plain123"},
	}
	req = httptest.NewRequest("POST", "/register", strings.NewReader(regForm.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	resp = w.Result()

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("Expected 303 redirect on register, got %d", resp.StatusCode)
	}
	cookies := resp.Cookies()
	var sessionCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == auth.SessionCookieName {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil || sessionCookie.Value == "" {
		t.Fatalf("Expected session cookie to be set on register")
	}

	// 3. 验收：重复用户名被拒（大小写不敏感）
	dupForm := url.Values{
		"username":     {"alice_user"}, // 同名小写
		"display_name": {"另一爱丽丝"},
		"password":     {"password2"},
	}
	req = httptest.NewRequest("POST", "/register", strings.NewReader(dupForm.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if !strings.Contains(w.Body.String(), "该用户名已被注册") {
		t.Fatalf("Expected duplicate username rejection message in body, got: %s", w.Body.String())
	}

	// 4. 登出
	req = httptest.NewRequest("POST", "/logout", nil)
	req.AddCookie(sessionCookie)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	resp = w.Result()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("Expected 303 on logout, got %d", resp.StatusCode)
	}
	var clearedCookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == auth.SessionCookieName {
			clearedCookie = c
			break
		}
	}
	if clearedCookie == nil || clearedCookie.MaxAge >= 0 {
		t.Fatalf("Expected session cookie to be cleared on logout")
	}

	// 5. 登录
	loginForm := url.Values{
		"username": {"Alice_User"},
		"password": {"plain123"},
	}
	req = httptest.NewRequest("POST", "/login", strings.NewReader(loginForm.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	resp = w.Result()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("Expected 303 on login, got %d", resp.StatusCode)
	}
	for _, c := range resp.Cookies() {
		if c.Name == auth.SessionCookieName {
			sessionCookie = c
			break
		}
	}

	// 6. 访问受保护页面 /me
	req = httptest.NewRequest("GET", "/me", nil)
	req.AddCookie(sessionCookie)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	resp = w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 on /me, got %d", resp.StatusCode)
	}
	bodyMe := w.Body.String()
	if !strings.Contains(bodyMe, "爱丽丝") || !strings.Contains(bodyMe, "alice_user") {
		t.Fatalf("Expected /me to display display name and username")
	}

	// 7. 验收：头像上传后正确显示
	bodyBuf := &bytes.Buffer{}
	mpWriter := multipart.NewWriter(bodyBuf)
	_ = mpWriter.WriteField("display_name", "爱丽丝二号")
	part, err := mpWriter.CreateFormFile("avatar", "test.png")
	if err != nil {
		t.Fatalf("Failed to create form file: %v", err)
	}
	fakePNG := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0}
	_, _ = part.Write(fakePNG)
	mpWriter.Close()

	req = httptest.NewRequest("POST", "/me/profile", bodyBuf)
	req.Header.Set("Content-Type", mpWriter.FormDataContentType())
	req.AddCookie(sessionCookie)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	resp = w.Result()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("Expected 303 on profile update, got %d", resp.StatusCode)
	}

	// 验证头像文件已被保存并可拉取
	u, err := auth.GetUserByID(database, 1)
	if err != nil || u.AvatarKey == nil {
		t.Fatalf("Expected user to have avatar key, got err=%v", err)
	}
	if !strings.HasPrefix(*u.AvatarKey, "avatars/") {
		t.Fatalf("Expected avatar key to start with avatars/, got %s", *u.AvatarKey)
	}

	req = httptest.NewRequest("GET", "/files/avatar?key="+url.QueryEscape(*u.AvatarKey), nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	resp = w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 on avatar fetch, got %d", resp.StatusCode)
	}
	avatarContent, _ := io.ReadAll(w.Body)
	if !bytes.Equal(avatarContent, fakePNG) {
		t.Fatalf("Fetched avatar does not match uploaded content")
	}

	// 8. 验收：改密码后旧密码失效
	pwdForm := url.Values{
		"old_password": {"plain123"},
		"new_password": {"newpass456"},
	}
	req = httptest.NewRequest("POST", "/me/password", strings.NewReader(pwdForm.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(sessionCookie)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	resp = w.Result()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("Expected 303 on password change, got %d", resp.StatusCode)
	}

	// 尝试用旧密码登录 → 失败
	oldLoginForm := url.Values{
		"username": {"alice_user"},
		"password": {"plain123"},
	}
	req = httptest.NewRequest("POST", "/login", strings.NewReader(oldLoginForm.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if !strings.Contains(w.Body.String(), "用户名或密码错误") {
		t.Fatalf("Expected old password to fail authentication")
	}

	// 尝试用新密码登录 → 成功
	newLoginForm := url.Values{
		"username": {"alice_user"},
		"password": {"newpass456"},
	}
	req = httptest.NewRequest("POST", "/login", strings.NewReader(newLoginForm.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	resp = w.Result()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("Expected 303 on new password login, got %d", resp.StatusCode)
	}
	_ = cfg
}
