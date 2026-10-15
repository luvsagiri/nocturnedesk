package web

import (
	"crypto/rand"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/luvsagiri/nocturnedesk/internal/auth"
	"github.com/luvsagiri/nocturnedesk/internal/user"
)

func (s *Server) handleMeGet(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFromContext(r.Context())
	if u == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	cards, _ := user.GetMyCards(s.db, u.ID)
	calEntries, _ := user.GetMyCalendarEntries(s.db, u.ID)
	docs, _ := user.GetMyDocs(s.db, u.ID)

	ctx := s.newViewContext(r, "个人中心", "me")
	ctx["Cards"] = cards
	ctx["CalendarEntries"] = calEntries
	ctx["Docs"] = docs

	if err := s.renderer.Render(w, "me", ctx); err != nil {
		s.RenderError(w, http.StatusInternalServerError, "渲染个人中心失败", err.Error())
	}
}

func (s *Server) handleMeProfilePost(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFromContext(r.Context())
	if u == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// 限制 2MB 上传
	if err := r.ParseMultipartForm(2 << 20); err != nil {
		redirectWithError(w, r, "/me", "头像文件过大，请上传不超过 2MB 的图片")
		return
	}

	displayName := r.FormValue("display_name")
	if strings.TrimSpace(displayName) == "" {
		redirectWithError(w, r, "/me", "显示名称不能为空")
		return
	}

	var newAvatarKey *string = u.AvatarKey

	file, header, err := r.FormFile("avatar")
	if err == nil && file != nil {
		defer file.Close()

		if header.Size > 2*1024*1024 {
			redirectWithError(w, r, "/me", "头像文件大小不能超过 2MB")
			return
		}

		ext := strings.ToLower(filepath.Ext(header.Filename))
		if ext != ".jpg" && ext != ".jpeg" && ext != ".png" && ext != ".webp" {
			redirectWithError(w, r, "/me", "头像仅支持 JPG、PNG 或 WEBP 格式")
			return
		}

		randomBytes := make([]byte, 16)
		_, _ = rand.Read(randomBytes)
		avatarFileName := hex.EncodeToString(randomBytes) + ext

		avatarsDir := filepath.Join(s.cfg.UploadsDir(), "avatars")
		if err := os.MkdirAll(avatarsDir, 0755); err != nil {
			redirectWithError(w, r, "/me", "服务器保存头像失败")
			return
		}

		destPath := filepath.Join(avatarsDir, avatarFileName)
		destFile, err := os.Create(destPath)
		if err != nil {
			redirectWithError(w, r, "/me", "创建头像文件失败")
			return
		}
		defer destFile.Close()

		if _, err := io.Copy(destFile, file); err != nil {
			redirectWithError(w, r, "/me", "保存头像文件内容失败")
			return
		}

		key := filepath.ToSlash(filepath.Join("avatars", avatarFileName))
		newAvatarKey = &key
	}

	if err := auth.UpdateProfile(s.db, u.ID, displayName, newAvatarKey); err != nil {
		redirectWithError(w, r, "/me", err.Error())
		return
	}

	redirectWithSuccess(w, r, "/me", "个人资料更新成功")
}

func (s *Server) handleMePasswordPost(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFromContext(r.Context())
	if u == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	if err := r.ParseForm(); err != nil {
		redirectWithError(w, r, "/me", "表单解析失败")
		return
	}

	oldPassword := r.FormValue("old_password")
	newPassword := r.FormValue("new_password")

	if err := auth.UpdatePassword(s.db, u.ID, oldPassword, newPassword); err != nil {
		redirectWithError(w, r, "/me", err.Error())
		return
	}

	redirectWithSuccess(w, r, "/me", "密码修改成功")
}

func (s *Server) handleAvatarFileGet(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")
	if key == "" {
		http.NotFound(w, r)
		return
	}

	// 安全校验，防止目录穿越
	cleanKey := filepath.Clean(key)
	if strings.Contains(cleanKey, "..") {
		http.NotFound(w, r)
		return
	}

	fullPath := filepath.Join(s.cfg.UploadsDir(), cleanKey)
	rel, err := filepath.Rel(s.cfg.UploadsDir(), fullPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		http.NotFound(w, r)
		return
	}

	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		http.NotFound(w, r)
		return
	}

	http.ServeFile(w, r, fullPath)
}
