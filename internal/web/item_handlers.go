package web

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	stdmime "mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/luvsagiri/nocturnedesk/internal/auth"
	"github.com/luvsagiri/nocturnedesk/internal/item"
	"github.com/luvsagiri/nocturnedesk/internal/project"
)

func detectMimeType(filename, headerMime string) string {
	if headerMime != "" && headerMime != "application/octet-stream" {
		return headerMime
	}
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".mp4":
		return "video/mp4"
	case ".webm":
		return "video/webm"
	case ".ogg", ".ogv":
		return "video/ogg"
	case ".mov":
		return "video/quicktime"
	case ".mp3":
		return "audio/mpeg"
	case ".wav":
		return "audio/wav"
	case ".m4a":
		return "audio/mp4"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".svg":
		return "image/svg+xml"
	case ".pdf":
		return "application/pdf"
	case ".md", ".markdown":
		return "text/markdown"
	case ".txt":
		return "text/plain"
	case ".json":
		return "application/json"
	default:
		if t := stdmime.TypeByExtension(ext); t != "" {
			return t
		}
	}
	return "application/octet-stream"
}

func (s *Server) handleProjectFilesGet(w http.ResponseWriter, r *http.Request) {
	p, m, ok := s.getProjectAndMember(w, r)
	if !ok {
		return
	}

	var folderID *int64
	folderParam := r.URL.Query().Get("folder")
	if folderParam != "" {
		if fid, err := strconv.ParseInt(folderParam, 10, 64); err == nil {
			folderID = &fid
		}
	}

	filter := r.URL.Query().Get("filter")
	if filter == "" {
		filter = "all"
	}

	var currentFolder *item.Folder
	if folderID != nil {
		cf, err := item.GetFolder(s.db, *folderID)
		if err == nil && cf.ProjectID == p.ID {
			currentFolder = cf
		} else {
			folderID = nil
		}
	}

	subfolders, err := item.ListSubfolders(s.db, p.ID, folderID)
	if err != nil {
		s.RenderError(w, http.StatusInternalServerError, "获取子文件夹失败", err.Error())
		return
	}

	allFolders, _ := item.ListFoldersInProject(s.db, p.ID)
	breadcrumbs, _ := item.GetBreadcrumbs(s.db, folderID)
	items, err := item.ListItems(s.db, p.ID, folderID, filter)
	if err != nil {
		s.RenderError(w, http.StatusInternalServerError, "获取条目列表失败", err.Error())
		return
	}

	var selectedItem *item.Item
	itemParam := r.URL.Query().Get("item")
	if itemParam != "" {
		if iid, err := strconv.ParseInt(itemParam, 10, 64); err == nil {
			if it, err := item.GetItem(s.db, iid); err == nil && it.ProjectID == p.ID {
				selectedItem = it
			}
		}
	}
	if selectedItem == nil && len(items) > 0 {
		selectedItem = &items[0]
	}

	ctx := s.newViewContext(r, p.Name+" · 文件夹", "projects")
	ctx["Project"] = p
	ctx["CurrentMember"] = m
	ctx["CurrentFolder"] = currentFolder
	ctx["Subfolders"] = subfolders
	ctx["AllFolders"] = allFolders
	ctx["Breadcrumbs"] = breadcrumbs
	ctx["Items"] = items
	ctx["SelectedItem"] = selectedItem
	ctx["Filter"] = filter
	ctx["MaxUploadMB"] = s.cfg.MaxUploadMB

	if err := s.renderer.Render(w, "project_files", ctx); err != nil {
		s.RenderError(w, http.StatusInternalServerError, "渲染文件夹页面失败", err.Error())
	}
}

func (s *Server) handleFolderCreatePost(w http.ResponseWriter, r *http.Request) {
	p, _, ok := s.getProjectAndMember(w, r)
	if !ok {
		return
	}

	if err := r.ParseForm(); err != nil {
		redirectWithError(w, r, fmt.Sprintf("/projects/%d/files", p.ID), "表单解析失败")
		return
	}

	name := r.FormValue("name")
	var parentID *int64
	parentStr := r.FormValue("parent_id")
	if parentStr != "" {
		if pid, err := strconv.ParseInt(parentStr, 10, 64); err == nil {
			parentID = &pid
		}
	}

	_, err := item.CreateFolder(s.db, p.ID, parentID, name)
	redirectURL := fmt.Sprintf("/projects/%d/files", p.ID)
	if parentID != nil {
		redirectURL = fmt.Sprintf("/projects/%d/files?folder=%d", p.ID, *parentID)
	}

	if err != nil {
		redirectWithError(w, r, redirectURL, err.Error())
		return
	}

	redirectWithSuccess(w, r, redirectURL, "文件夹创建成功")
}

func (s *Server) handleFolderRenamePost(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFromContext(r.Context())
	if u == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	fidStr := r.PathValue("fid")
	fid, err := strconv.ParseInt(fidStr, 10, 64)
	if err != nil {
		s.RenderError(w, http.StatusNotFound, "文件夹不存在", "ID格式错误")
		return
	}

	f, err := item.GetFolder(s.db, fid)
	if err != nil {
		s.RenderError(w, http.StatusNotFound, "文件夹不存在", err.Error())
		return
	}

	// 校验项目成员可见性
	_, _, err = project.GetProjectDetails(s.db, f.ProjectID, u.ID)
	if err != nil {
		s.RenderError(w, http.StatusNotFound, "项目不存在", "你无权访问该项目")
		return
	}

	if err := r.ParseForm(); err != nil {
		redirectWithError(w, r, fmt.Sprintf("/projects/%d/files", f.ProjectID), "表单解析失败")
		return
	}

	newName := r.FormValue("name")
	if err := item.RenameFolder(s.db, fid, newName); err != nil {
		redirectWithError(w, r, fmt.Sprintf("/projects/%d/files", f.ProjectID), err.Error())
		return
	}

	redirectWithSuccess(w, r, fmt.Sprintf("/projects/%d/files", f.ProjectID), "文件夹已重命名")
}

func (s *Server) handleFolderMovePost(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFromContext(r.Context())
	if u == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	fidStr := r.PathValue("fid")
	fid, err := strconv.ParseInt(fidStr, 10, 64)
	if err != nil {
		s.RenderError(w, http.StatusNotFound, "文件夹不存在", "ID格式错误")
		return
	}

	f, err := item.GetFolder(s.db, fid)
	if err != nil {
		s.RenderError(w, http.StatusNotFound, "文件夹不存在", err.Error())
		return
	}

	_, _, err = project.GetProjectDetails(s.db, f.ProjectID, u.ID)
	if err != nil {
		s.RenderError(w, http.StatusNotFound, "项目不存在", "你无权访问该项目")
		return
	}

	if err := r.ParseForm(); err != nil {
		redirectWithError(w, r, fmt.Sprintf("/projects/%d/files", f.ProjectID), "表单解析失败")
		return
	}

	var targetParentID *int64
	targetStr := r.FormValue("target_parent_id")
	if targetStr != "" {
		if tID, err := strconv.ParseInt(targetStr, 10, 64); err == nil && tID > 0 {
			targetParentID = &tID
		}
	}

	if err := item.MoveFolder(s.db, fid, targetParentID); err != nil {
		redirectWithError(w, r, fmt.Sprintf("/projects/%d/files", f.ProjectID), err.Error())
		return
	}

	redirectWithSuccess(w, r, fmt.Sprintf("/projects/%d/files", f.ProjectID), "文件夹移动成功")
}

func (s *Server) handleFolderDeletePost(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFromContext(r.Context())
	if u == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	fidStr := r.PathValue("fid")
	fid, err := strconv.ParseInt(fidStr, 10, 64)
	if err != nil {
		s.RenderError(w, http.StatusNotFound, "文件夹不存在", "ID格式错误")
		return
	}

	f, err := item.GetFolder(s.db, fid)
	if err != nil {
		s.RenderError(w, http.StatusNotFound, "文件夹不存在", err.Error())
		return
	}

	_, _, err = project.GetProjectDetails(s.db, f.ProjectID, u.ID)
	if err != nil {
		s.RenderError(w, http.StatusNotFound, "项目不存在", "你无权访问该项目")
		return
	}

	if err := item.DeleteFolder(s.db, fid, s.cfg.DataDir); err != nil {
		redirectWithError(w, r, fmt.Sprintf("/projects/%d/files", f.ProjectID), err.Error())
		return
	}

	redirectWithSuccess(w, r, fmt.Sprintf("/projects/%d/files", f.ProjectID), "文件夹及其下内容已删除")
}

func (s *Server) handleDocNewGet(w http.ResponseWriter, r *http.Request) {
	p, m, ok := s.getProjectAndMember(w, r)
	if !ok {
		return
	}

	var folderID *int64
	folderParam := r.URL.Query().Get("folder")
	if folderParam != "" {
		if fid, err := strconv.ParseInt(folderParam, 10, 64); err == nil {
			folderID = &fid
		}
	}

	breadcrumbs, _ := item.GetBreadcrumbs(s.db, folderID)

	ctx := s.newViewContext(r, "新建文档 · "+p.Name, "projects")
	ctx["Project"] = p
	ctx["CurrentMember"] = m
	ctx["FolderID"] = folderID
	ctx["Breadcrumbs"] = breadcrumbs

	if err := s.renderer.Render(w, "doc_edit", ctx); err != nil {
		s.RenderError(w, http.StatusInternalServerError, "渲染新建文档页面失败", err.Error())
	}
}

func (s *Server) handleDocCreatePost(w http.ResponseWriter, r *http.Request) {
	p, _, ok := s.getProjectAndMember(w, r)
	if !ok {
		return
	}
	u := auth.UserFromContext(r.Context())

	if err := r.ParseForm(); err != nil {
		redirectWithError(w, r, fmt.Sprintf("/projects/%d/files", p.ID), "表单解析失败")
		return
	}

	title := r.FormValue("title")
	bodyHTML := r.FormValue("body_html")

	var folderID *int64
	folderStr := r.FormValue("folder_id")
	if folderStr != "" {
		if fid, err := strconv.ParseInt(folderStr, 10, 64); err == nil && fid > 0 {
			folderID = &fid
		}
	}

	docItem, err := item.CreateDoc(s.db, p.ID, folderID, u.ID, title, bodyHTML)
	if err != nil {
		redirectWithError(w, r, fmt.Sprintf("/projects/%d/docs/new", p.ID), err.Error())
		return
	}

	http.Redirect(w, r, fmt.Sprintf("/projects/%d/docs/%d", p.ID, docItem.ID), http.StatusSeeOther)
}

func (s *Server) handleDocViewGet(w http.ResponseWriter, r *http.Request) {
	p, m, ok := s.getProjectAndMember(w, r)
	if !ok {
		return
	}

	iidStr := r.PathValue("iid")
	iid, err := strconv.ParseInt(iidStr, 10, 64)
	if err != nil {
		s.RenderError(w, http.StatusNotFound, "文档不存在", "ID格式错误")
		return
	}

	docItem, err := item.GetItem(s.db, iid)
	if err != nil || docItem.ProjectID != p.ID || docItem.Kind != "doc" {
		s.RenderError(w, http.StatusNotFound, "文档不存在", "该文档不存在或已被删除")
		return
	}

	breadcrumbs, _ := item.GetBreadcrumbs(s.db, docItem.FolderID)

	ctx := s.newViewContext(r, docItem.Title+" · "+p.Name, "projects")
	ctx["Project"] = p
	ctx["CurrentMember"] = m
	ctx["Item"] = docItem
	ctx["Breadcrumbs"] = breadcrumbs

	if err := s.renderer.Render(w, "doc_view", ctx); err != nil {
		s.RenderError(w, http.StatusInternalServerError, "渲染文档失败", err.Error())
	}
}

func (s *Server) handleDocEditGet(w http.ResponseWriter, r *http.Request) {
	p, m, ok := s.getProjectAndMember(w, r)
	if !ok {
		return
	}

	iidStr := r.PathValue("iid")
	iid, err := strconv.ParseInt(iidStr, 10, 64)
	if err != nil {
		s.RenderError(w, http.StatusNotFound, "文档不存在", "ID格式错误")
		return
	}

	docItem, err := item.GetItem(s.db, iid)
	if err != nil || docItem.ProjectID != p.ID || docItem.Kind != "doc" {
		s.RenderError(w, http.StatusNotFound, "文档不存在", "该文档不存在或已被删除")
		return
	}

	breadcrumbs, _ := item.GetBreadcrumbs(s.db, docItem.FolderID)

	ctx := s.newViewContext(r, "编辑文档 · "+docItem.Title, "projects")
	ctx["Project"] = p
	ctx["CurrentMember"] = m
	ctx["Item"] = docItem
	ctx["FolderID"] = docItem.FolderID
	ctx["Breadcrumbs"] = breadcrumbs

	if err := s.renderer.Render(w, "doc_edit", ctx); err != nil {
		s.RenderError(w, http.StatusInternalServerError, "渲染编辑文档页面失败", err.Error())
	}
}

func (s *Server) handleItemUpdatePost(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFromContext(r.Context())
	if u == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	iidStr := r.PathValue("iid")
	iid, err := strconv.ParseInt(iidStr, 10, 64)
	if err != nil {
		s.RenderError(w, http.StatusNotFound, "条目不存在", "ID格式错误")
		return
	}

	it, err := item.GetItem(s.db, iid)
	if err != nil {
		s.RenderError(w, http.StatusNotFound, "条目不存在", err.Error())
		return
	}

	_, _, err = project.GetProjectDetails(s.db, it.ProjectID, u.ID)
	if err != nil {
		s.RenderError(w, http.StatusNotFound, "项目不存在", "你无权访问该项目")
		return
	}

	if err := r.ParseForm(); err != nil {
		redirectWithError(w, r, fmt.Sprintf("/projects/%d/files", it.ProjectID), "表单解析失败")
		return
	}

	title := r.FormValue("title")
	bodyHTML := r.FormValue("body_html")

	var folderID *int64 = it.FolderID
	folderStr := r.FormValue("folder_id")
	if folderStr != "" {
		if fid, err := strconv.ParseInt(folderStr, 10, 64); err == nil {
			if fid > 0 {
				folderID = &fid
			} else {
				folderID = nil
			}
		}
	}

	if it.Kind == "doc" {
		if err := item.UpdateDoc(s.db, iid, title, bodyHTML, folderID); err != nil {
			redirectWithError(w, r, fmt.Sprintf("/projects/%d/docs/%d/edit", it.ProjectID, iid), err.Error())
			return
		}
		http.Redirect(w, r, fmt.Sprintf("/projects/%d/files?item=%d", it.ProjectID, iid), http.StatusSeeOther)
		return
	}

	if it.Kind == "file" && it.IsTextOrMarkdown() {
		if err := item.UpdateFileText(s.db, iid, title, bodyHTML, folderID, s.cfg.DataDir); err != nil {
			redirectWithError(w, r, fmt.Sprintf("/projects/%d/files?item=%d", it.ProjectID, iid), err.Error())
			return
		}
		redirectWithSuccess(w, r, fmt.Sprintf("/projects/%d/files?item=%d", it.ProjectID, iid), "文件保存成功")
		return
	}

	redirectWithSuccess(w, r, fmt.Sprintf("/projects/%d/files", it.ProjectID), "保存成功")
}

func (s *Server) handleItemDeletePost(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFromContext(r.Context())
	if u == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	iidStr := r.PathValue("iid")
	iid, err := strconv.ParseInt(iidStr, 10, 64)
	if err != nil {
		s.RenderError(w, http.StatusNotFound, "条目不存在", "ID格式错误")
		return
	}

	it, err := item.GetItem(s.db, iid)
	if err != nil {
		s.RenderError(w, http.StatusNotFound, "条目不存在", err.Error())
		return
	}

	_, _, err = project.GetProjectDetails(s.db, it.ProjectID, u.ID)
	if err != nil {
		s.RenderError(w, http.StatusNotFound, "项目不存在", "你无权访问该项目")
		return
	}

	if err := item.DeleteItem(s.db, iid, s.cfg.DataDir); err != nil {
		redirectWithError(w, r, fmt.Sprintf("/projects/%d/files", it.ProjectID), err.Error())
		return
	}

	redirectWithSuccess(w, r, fmt.Sprintf("/projects/%d/files", it.ProjectID), "条目已成功删除")
}

func (s *Server) handleDocDownloadGet(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFromContext(r.Context())
	if u == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	iidStr := r.PathValue("iid")
	iid, err := strconv.ParseInt(iidStr, 10, 64)
	if err != nil {
		s.RenderError(w, http.StatusNotFound, "文档不存在", "ID格式错误")
		return
	}

	it, err := item.GetItem(s.db, iid)
	if err != nil || it.Kind != "doc" {
		s.RenderError(w, http.StatusNotFound, "文档不存在", "文档不存在或已被删除")
		return
	}

	_, _, err = project.GetProjectDetails(s.db, it.ProjectID, u.ID)
	if err != nil {
		s.RenderError(w, http.StatusNotFound, "项目不存在", "你无权访问该项目")
		return
	}

	filename := it.Title
	if !strings.HasSuffix(strings.ToLower(filename), ".md") {
		filename += ".md"
	}

	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	content := ""
	if it.BodyHTML != nil {
		content = *it.BodyHTML
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(content)))
	_, _ = w.Write([]byte(content))
}

func (s *Server) handleFileUploadPost(w http.ResponseWriter, r *http.Request) {
	p, _, ok := s.getProjectAndMember(w, r)
	if !ok {
		return
	}
	u := auth.UserFromContext(r.Context())

	isAJAX := r.Header.Get("X-Requested-With") == "XMLHttpRequest" || strings.Contains(r.Header.Get("Accept"), "application/json")

	sendError := func(msg string, code int) {
		if isAJAX {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(code)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": msg})
		} else {
			redirectWithError(w, r, fmt.Sprintf("/projects/%d/files", p.ID), msg)
		}
	}

	sendSuccess := func(redirectURL string, itemID int64) {
		if isAJAX {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "redirect": redirectURL, "id": itemID})
		} else {
			redirectWithSuccess(w, r, redirectURL, "资源文件上传成功")
		}
	}

	maxBytes := s.cfg.MaxUploadBytes()
	// 前置检查已知 ContentLength，避免超大文件继续写入
	if r.ContentLength > maxBytes+16*1024*1024 {
		sendError(fmt.Sprintf("文件大小超过上限（单个文件最大允许 %dMB）", s.cfg.MaxUploadMB), http.StatusBadRequest)
		return
	}

	// 优先使用 MultipartReader 进行无缓冲流式写入，不使用 MaxBytesReader 暴力打断 TCP 连接，彻底根治连接重置
	mr, err := r.MultipartReader()
	if err == nil {
		var folderID *int64
		var filename, mimeType string
		var written int64
		var relStorageKey string
		var fileFound bool

		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				break
			}

			formName := part.FormName()
			if formName == "folder_id" {
				buf := new(strings.Builder)
				_, _ = io.Copy(buf, part)
				if fid, err := strconv.ParseInt(strings.TrimSpace(buf.String()), 10, 64); err == nil && fid > 0 {
					folderID = &fid
				}
				continue
			}

			if formName == "file" && part.FileName() != "" {
				filename = part.FileName()
				mimeType = detectMimeType(filename, part.Header.Get("Content-Type"))

				randomBytes := make([]byte, 16)
				_, _ = rand.Read(randomBytes)
				ext := filepath.Ext(filename)
				storageFileName := hex.EncodeToString(randomBytes) + ext

				projectUploadDir := filepath.Join(s.cfg.UploadsDir(), strconv.FormatInt(p.ID, 10))
				_ = os.MkdirAll(projectUploadDir, 0755)

				fullDestPath := filepath.Join(projectUploadDir, storageFileName)
				destFile, err := os.Create(fullDestPath)
				if err != nil {
					sendError("创建存储文件失败", http.StatusInternalServerError)
					return
				}

				// 使用 LimitReader 保护磁盘上限，超出上限时正常返回错误信息，不破坏 TCP 传输层
				n, copyErr := io.Copy(destFile, io.LimitReader(part, maxBytes+1))
				destFile.Close()

				if copyErr != nil {
					_ = os.Remove(fullDestPath)
					sendError("写入文件数据失败", http.StatusInternalServerError)
					return
				}

				if n > maxBytes {
					_ = os.Remove(fullDestPath)
					sendError(fmt.Sprintf("文件大小超过上限（单个文件最大允许 %dMB）", s.cfg.MaxUploadMB), http.StatusBadRequest)
					return
				}

				written = n
				relStorageKey = filepath.ToSlash(filepath.Join("uploads", strconv.FormatInt(p.ID, 10), storageFileName))
				fileFound = true
				break
			}
		}

		if fileFound {
			newItem, err := item.CreateFile(s.db, p.ID, folderID, u.ID, filename, mimeType, written, relStorageKey)
			if err != nil {
				sendError(err.Error(), http.StatusInternalServerError)
				return
			}

			redirectURL := fmt.Sprintf("/projects/%d/files", p.ID)
			if folderID != nil {
				redirectURL = fmt.Sprintf("/projects/%d/files?folder=%d&item=%d", p.ID, *folderID, newItem.ID)
			} else {
				redirectURL = fmt.Sprintf("/projects/%d/files?item=%d", p.ID, newItem.ID)
			}

			sendSuccess(redirectURL, newItem.ID)
			return
		}
	}

	// 兼容普通 MultipartForm 解析
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		sendError(fmt.Sprintf("文件大小超过上限（单个文件最大允许 %dMB）", s.cfg.MaxUploadMB), http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		sendError("未选择有效文件", http.StatusBadRequest)
		return
	}
	defer file.Close()

	if header.Size > maxBytes {
		sendError(fmt.Sprintf("文件大小超过上限（单个文件最大允许 %dMB）", s.cfg.MaxUploadMB), http.StatusBadRequest)
		return
	}

	var folderID *int64
	folderStr := r.FormValue("folder_id")
	if folderStr != "" {
		if fid, err := strconv.ParseInt(folderStr, 10, 64); err == nil && fid > 0 {
			folderID = &fid
		}
	}

	randomBytes := make([]byte, 16)
	_, _ = rand.Read(randomBytes)
	ext := filepath.Ext(header.Filename)
	storageFileName := hex.EncodeToString(randomBytes) + ext

	projectUploadDir := filepath.Join(s.cfg.UploadsDir(), strconv.FormatInt(p.ID, 10))
	_ = os.MkdirAll(projectUploadDir, 0755)

	fullDestPath := filepath.Join(projectUploadDir, storageFileName)
	destFile, err := os.Create(fullDestPath)
	if err != nil {
		sendError("保存文件失败", http.StatusInternalServerError)
		return
	}
	defer destFile.Close()

	written, err := io.Copy(destFile, file)
	if err != nil {
		_ = os.Remove(fullDestPath)
		sendError("写入文件数据失败", http.StatusInternalServerError)
		return
	}

	relStorageKey := filepath.ToSlash(filepath.Join("uploads", strconv.FormatInt(p.ID, 10), storageFileName))
	mimeType := detectMimeType(header.Filename, header.Header.Get("Content-Type"))

	newItem, err := item.CreateFile(s.db, p.ID, folderID, u.ID, header.Filename, mimeType, written, relStorageKey)
	if err != nil {
		_ = os.Remove(fullDestPath)
		sendError(err.Error(), http.StatusInternalServerError)
		return
	}

	redirectURL := fmt.Sprintf("/projects/%d/files", p.ID)
	if folderID != nil {
		redirectURL = fmt.Sprintf("/projects/%d/files?folder=%d&item=%d", p.ID, *folderID, newItem.ID)
	} else {
		redirectURL = fmt.Sprintf("/projects/%d/files?item=%d", p.ID, newItem.ID)
	}

	sendSuccess(redirectURL, newItem.ID)
}
