package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/luvsagiri/nocturnedesk/internal/auth"
	"github.com/luvsagiri/nocturnedesk/internal/board"
	"github.com/luvsagiri/nocturnedesk/internal/project"
)

func (s *Server) handleProjectBoardGet(w http.ResponseWriter, r *http.Request) {
	p, m, ok := s.getProjectAndMember(w, r)
	if !ok {
		return
	}

	boardView, err := board.GetBoardView(s.db, p.ID)
	if err != nil {
		s.RenderError(w, http.StatusInternalServerError, "获取看板失败", err.Error())
		return
	}

	ctx := s.newViewContext(r, p.Name+" · 看板", "projects")
	ctx["Project"] = p
	ctx["CurrentMember"] = m
	ctx["BoardView"] = boardView
	ctx["ActiveTab"] = "board"

	if err := s.renderer.Render(w, "project_board", ctx); err != nil {
		s.RenderError(w, http.StatusInternalServerError, "渲染看板页面失败", err.Error())
	}
}

func (s *Server) handleCardCreatePost(w http.ResponseWriter, r *http.Request) {
	p, _, ok := s.getProjectAndMember(w, r)
	if !ok {
		return
	}
	u := auth.UserFromContext(r.Context())

	if err := r.ParseForm(); err != nil {
		redirectWithError(w, r, fmt.Sprintf("/projects/%d/board", p.ID), "表单解析失败")
		return
	}

	title := r.FormValue("title")
	description := r.FormValue("description")

	var assigneeID *int64
	assigneeStr := r.FormValue("assignee_id")
	if assigneeStr != "" {
		if aid, err := strconv.ParseInt(assigneeStr, 10, 64); err == nil && aid > 0 {
			assigneeID = &aid
		}
	}

	var dueDate *string
	dueDateStr := strings.TrimSpace(r.FormValue("due_date"))
	if dueDateStr != "" {
		dueDate = &dueDateStr
	}

	labelIDs := parseLabelIDs(r.Form["label_ids"])

	_, err := board.CreateCard(s.db, p.ID, u.ID, title, description, assigneeID, dueDate, labelIDs)
	if err != nil {
		redirectWithError(w, r, fmt.Sprintf("/projects/%d/board", p.ID), err.Error())
		return
	}

	redirectWithSuccess(w, r, fmt.Sprintf("/projects/%d/board", p.ID), "卡片创建成功")
}

func (s *Server) handleCardUpdatePost(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFromContext(r.Context())
	if u == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	cidStr := r.PathValue("cid")
	cid, err := strconv.ParseInt(cidStr, 10, 64)
	if err != nil {
		s.RenderError(w, http.StatusNotFound, "卡片不存在", "ID格式错误")
		return
	}

	c, err := board.GetCard(s.db, cid)
	if err != nil {
		s.RenderError(w, http.StatusNotFound, "卡片不存在", err.Error())
		return
	}

	_, _, err = project.GetProjectDetails(s.db, c.ProjectID, u.ID)
	if err != nil {
		s.RenderError(w, http.StatusNotFound, "项目不存在", "你无权访问该项目")
		return
	}

	if err := r.ParseForm(); err != nil {
		redirectWithError(w, r, fmt.Sprintf("/projects/%d/board", c.ProjectID), "表单解析失败")
		return
	}

	title := r.FormValue("title")
	description := r.FormValue("description")

	var assigneeID *int64
	assigneeStr := r.FormValue("assignee_id")
	if assigneeStr != "" {
		if aid, err := strconv.ParseInt(assigneeStr, 10, 64); err == nil && aid > 0 {
			assigneeID = &aid
		}
	}

	var dueDate *string
	dueDateStr := strings.TrimSpace(r.FormValue("due_date"))
	if dueDateStr != "" {
		dueDate = &dueDateStr
	}

	labelIDs := parseLabelIDs(r.Form["label_ids"])

	if err := board.UpdateCard(s.db, cid, title, description, assigneeID, dueDate, labelIDs); err != nil {
		redirectWithError(w, r, fmt.Sprintf("/projects/%d/board", c.ProjectID), err.Error())
		return
	}

	redirectWithSuccess(w, r, fmt.Sprintf("/projects/%d/board", c.ProjectID), "卡片更新成功")
}

func (s *Server) handleCardMovePost(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFromContext(r.Context())
	if u == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	cidStr := r.PathValue("cid")
	cid, err := strconv.ParseInt(cidStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid card ID", http.StatusBadRequest)
		return
	}

	c, err := board.GetCard(s.db, cid)
	if err != nil {
		http.Error(w, "Card not found", http.StatusNotFound)
		return
	}

	_, _, err = project.GetProjectDetails(s.db, c.ProjectID, u.ID)
	if err != nil {
		http.Error(w, "Forbidden", http.StatusNotFound)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	status := r.FormValue("status")
	var prevCardID *int64
	if prevStr := r.FormValue("prev_card_id"); prevStr != "" {
		if pid, err := strconv.ParseInt(prevStr, 10, 64); err == nil && pid > 0 {
			prevCardID = &pid
		}
	}

	var nextCardID *int64
	if nextStr := r.FormValue("next_card_id"); nextStr != "" {
		if nid, err := strconv.ParseInt(nextStr, 10, 64); err == nil && nid > 0 {
			nextCardID = &nid
		}
	}

	if err := board.MoveCard(s.db, cid, status, prevCardID, nextCardID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func (s *Server) handleCardDeletePost(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFromContext(r.Context())
	if u == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	cidStr := r.PathValue("cid")
	cid, err := strconv.ParseInt(cidStr, 10, 64)
	if err != nil {
		s.RenderError(w, http.StatusNotFound, "卡片不存在", "ID格式错误")
		return
	}

	c, err := board.GetCard(s.db, cid)
	if err != nil {
		s.RenderError(w, http.StatusNotFound, "卡片不存在", err.Error())
		return
	}

	_, _, err = project.GetProjectDetails(s.db, c.ProjectID, u.ID)
	if err != nil {
		s.RenderError(w, http.StatusNotFound, "项目不存在", "你无权访问该项目")
		return
	}

	if err := board.DeleteCard(s.db, cid); err != nil {
		redirectWithError(w, r, fmt.Sprintf("/projects/%d/board", c.ProjectID), err.Error())
		return
	}

	redirectWithSuccess(w, r, fmt.Sprintf("/projects/%d/board", c.ProjectID), "卡片已删除")
}

func (s *Server) handleProjectLabelsGet(w http.ResponseWriter, r *http.Request) {
	p, _, ok := s.getProjectAndMember(w, r)
	if !ok {
		return
	}

	labels, err := board.ListLabels(s.db, p.ID)
	if err != nil {
		s.RenderError(w, http.StatusInternalServerError, "获取标签列表失败", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(labels)
}

func (s *Server) handleProjectLabelCreatePost(w http.ResponseWriter, r *http.Request) {
	p, _, ok := s.getProjectAndMember(w, r)
	if !ok {
		return
	}

	if err := r.ParseForm(); err != nil {
		redirectWithError(w, r, fmt.Sprintf("/projects/%d/board", p.ID), "表单解析失败")
		return
	}

	name := r.FormValue("name")
	color := r.FormValue("color")

	_, err := board.CreateLabel(s.db, p.ID, name, color)
	if err != nil {
		redirectWithError(w, r, fmt.Sprintf("/projects/%d/board", p.ID), err.Error())
		return
	}

	redirectWithSuccess(w, r, fmt.Sprintf("/projects/%d/board", p.ID), "标签创建成功")
}

func (s *Server) handleLabelUpdatePost(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFromContext(r.Context())
	if u == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	lidStr := r.PathValue("lid")
	lid, err := strconv.ParseInt(lidStr, 10, 64)
	if err != nil {
		s.RenderError(w, http.StatusNotFound, "标签不存在", "ID格式错误")
		return
	}

	var pid int64
	err = s.db.QueryRow("SELECT project_id FROM labels WHERE id = ?", lid).Scan(&pid)
	if err != nil {
		s.RenderError(w, http.StatusNotFound, "标签不存在", "标签不存在")
		return
	}

	_, _, err = project.GetProjectDetails(s.db, pid, u.ID)
	if err != nil {
		s.RenderError(w, http.StatusNotFound, "项目不存在", "你无权访问该项目")
		return
	}

	if err := r.ParseForm(); err != nil {
		redirectWithError(w, r, fmt.Sprintf("/projects/%d/board", pid), "表单解析失败")
		return
	}

	name := r.FormValue("name")
	color := r.FormValue("color")

	if err := board.UpdateLabel(s.db, lid, name, color); err != nil {
		redirectWithError(w, r, fmt.Sprintf("/projects/%d/board", pid), err.Error())
		return
	}

	redirectWithSuccess(w, r, fmt.Sprintf("/projects/%d/board", pid), "标签更新成功")
}

func (s *Server) handleLabelDeletePost(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFromContext(r.Context())
	if u == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	lidStr := r.PathValue("lid")
	lid, err := strconv.ParseInt(lidStr, 10, 64)
	if err != nil {
		s.RenderError(w, http.StatusNotFound, "标签不存在", "ID格式错误")
		return
	}

	var pid int64
	err = s.db.QueryRow("SELECT project_id FROM labels WHERE id = ?", lid).Scan(&pid)
	if err != nil {
		s.RenderError(w, http.StatusNotFound, "标签不存在", "标签不存在")
		return
	}

	_, _, err = project.GetProjectDetails(s.db, pid, u.ID)
	if err != nil {
		s.RenderError(w, http.StatusNotFound, "项目不存在", "你无权访问该项目")
		return
	}

	if err := board.DeleteLabel(s.db, lid); err != nil {
		redirectWithError(w, r, fmt.Sprintf("/projects/%d/board", pid), err.Error())
		return
	}

	redirectWithSuccess(w, r, fmt.Sprintf("/projects/%d/board", pid), "标签已删除")
}

func parseLabelIDs(raw []string) []int64 {
	var ids []int64
	for _, s := range raw {
		if id, err := strconv.ParseInt(s, 10, 64); err == nil && id > 0 {
			ids = append(ids, id)
		}
	}
	return ids
}
