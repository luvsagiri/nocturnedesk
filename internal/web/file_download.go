package web

import (
	"database/sql"
	"errors"
	"fmt"
	stdmime "mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/luvsagiri/nocturnedesk/internal/auth"
	"github.com/luvsagiri/nocturnedesk/internal/project"
)

func (s *Server) handleFileDownloadGet(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFromContext(r.Context())
	if u == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	iidStr := r.PathValue("iid")
	iid, err := strconv.ParseInt(iidStr, 10, 64)
	if err != nil {
		s.RenderError(w, http.StatusNotFound, "文件未找到", "文件ID无效")
		return
	}

	var projectID int64
	var storageKey, originalName, mime sql.NullString
	err = s.db.QueryRow(`
		SELECT project_id, storage_key, original_name, mime
		FROM items
		WHERE id = ?
	`, iid).Scan(&projectID, &storageKey, &originalName, &mime)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.RenderError(w, http.StatusNotFound, "文件未找到", "该文件不存在或已被删除")
			return
		}
		s.RenderError(w, http.StatusInternalServerError, "查询文件失败", err.Error())
		return
	}

	// 可见性铁律：必须是该项目的成员；非成员直接返回 404，不泄露文件是否存在
	_, _, err = project.GetProjectDetails(s.db, projectID, u.ID)
	if err != nil {
		s.RenderError(w, http.StatusNotFound, "文件未找到", "该文件不存在或你无权访问")
		return
	}

	if !storageKey.Valid || storageKey.String == "" {
		s.RenderError(w, http.StatusNotFound, "文件未找到", "文件存储记录不存在")
		return
	}

	fullPath := filepath.Join(s.cfg.DataDir, storageKey.String)
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		s.RenderError(w, http.StatusNotFound, "文件未找到", "磁盘物理文件已丢失")
		return
	}

	if r.URL.Query().Get("download") == "1" && originalName.Valid && originalName.String != "" {
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", originalName.String))
	}

	contentType := ""
	if mime.Valid && mime.String != "" && mime.String != "application/octet-stream" {
		contentType = mime.String
	} else if originalName.Valid && originalName.String != "" {
		ext := strings.ToLower(filepath.Ext(originalName.String))
		switch ext {
		case ".mp4":
			contentType = "video/mp4"
		case ".webm":
			contentType = "video/webm"
		case ".ogg", ".ogv":
			contentType = "video/ogg"
		case ".mov":
			contentType = "video/quicktime"
		case ".mp3":
			contentType = "audio/mpeg"
		case ".wav":
			contentType = "audio/wav"
		case ".m4a":
			contentType = "audio/mp4"
		case ".jpg", ".jpeg":
			contentType = "image/jpeg"
		case ".png":
			contentType = "image/png"
		case ".gif":
			contentType = "image/gif"
		case ".webp":
			contentType = "image/webp"
		case ".svg":
			contentType = "image/svg+xml"
		case ".pdf":
			contentType = "application/pdf"
		default:
			contentType = stdmime.TypeByExtension(ext)
		}
	}
	if contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}

	w.Header().Set("Accept-Ranges", "bytes")

	http.ServeFile(w, r, fullPath)
}
