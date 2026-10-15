package web

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/luvsagiri/nocturnedesk/internal/auth"
	"github.com/luvsagiri/nocturnedesk/internal/project"
)

func (s *Server) getProjectAndMember(w http.ResponseWriter, r *http.Request) (*project.Project, *project.Member, bool) {
	u := auth.UserFromContext(r.Context())
	if u == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return nil, nil, false
	}

	pidStr := r.PathValue("pid")
	pid, err := strconv.ParseInt(pidStr, 10, 64)
	if err != nil {
		s.RenderError(w, http.StatusNotFound, "项目未找到", "项目ID不存在或格式错误")
		return nil, nil, false
	}

	p, m, err := project.GetProjectDetails(s.db, pid, u.ID)
	if err != nil {
		// 可见性铁律：非成员访问一律返回 404，防止探测项目是否存在
		s.RenderError(w, http.StatusNotFound, "项目未找到", "该项目不存在或你未受邀加入该项目")
		return nil, nil, false
	}

	return p, m, true
}

func (s *Server) handleProjectNewGet(w http.ResponseWriter, r *http.Request) {
	ctx := s.newViewContext(r, "新建项目", "projects")
	ctx["PaletteColors"] = project.PaletteColors
	if err := s.renderer.Render(w, "project_new", ctx); err != nil {
		s.RenderError(w, http.StatusInternalServerError, "渲染新建项目页面失败", err.Error())
	}
}

func (s *Server) handleProjectCreatePost(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFromContext(r.Context())
	if u == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	if err := r.ParseForm(); err != nil {
		redirectWithError(w, r, "/projects/new", "表单解析失败")
		return
	}

	name := r.FormValue("name")
	description := r.FormValue("description")
	color := r.FormValue("color")

	p, err := project.CreateProject(s.db, u.ID, name, description, color)
	if err != nil {
		redirectWithError(w, r, "/projects/new", err.Error())
		return
	}

	http.Redirect(w, r, fmt.Sprintf("/projects/%d/files", p.ID), http.StatusSeeOther)
}

func (s *Server) handleProjectJoinPost(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFromContext(r.Context())
	if u == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	if err := r.ParseForm(); err != nil {
		redirectWithError(w, r, "/", "表单解析失败")
		return
	}

	inviteCode := r.FormValue("invite_code")
	p, err := project.JoinProjectByInviteCode(s.db, u.ID, inviteCode)
	if err != nil {
		redirectWithError(w, r, "/", err.Error())
		return
	}

	http.Redirect(w, r, fmt.Sprintf("/projects/%d/files", p.ID), http.StatusSeeOther)
}

func (s *Server) handleProjectRootGet(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("pid")
	http.Redirect(w, r, fmt.Sprintf("/projects/%s/files", pid), http.StatusFound)
}

func (s *Server) handleProjectMembersGet(w http.ResponseWriter, r *http.Request) {
	p, m, ok := s.getProjectAndMember(w, r)
	if !ok {
		return
	}

	members, err := project.ListProjectMembers(s.db, p.ID)
	if err != nil {
		s.RenderError(w, http.StatusInternalServerError, "获取成员列表失败", err.Error())
		return
	}

	ctx := s.newViewContext(r, p.Name+" · 成员", "projects")
	ctx["Project"] = p
	ctx["CurrentMember"] = m
	ctx["Members"] = members
	ctx["PaletteColors"] = project.PaletteColors
	ctx["ActiveTab"] = "members"

	if err := s.renderer.Render(w, "project_members", ctx); err != nil {
		s.RenderError(w, http.StatusInternalServerError, "渲染成员页面失败", err.Error())
	}
}

func (s *Server) handleProjectMemberColorPost(w http.ResponseWriter, r *http.Request) {
	p, m, ok := s.getProjectAndMember(w, r)
	if !ok {
		return
	}

	if err := r.ParseForm(); err != nil {
		redirectWithError(w, r, fmt.Sprintf("/projects/%d/members", p.ID), "表单解析失败")
		return
	}

	color := r.FormValue("color")
	if err := project.UpdateMemberColor(s.db, p.ID, m.UserID, color); err != nil {
		redirectWithError(w, r, fmt.Sprintf("/projects/%d/members", p.ID), err.Error())
		return
	}

	redirectWithSuccess(w, r, fmt.Sprintf("/projects/%d/members", p.ID), "个人项目颜色已更新")
}

func (s *Server) handleProjectMemberRemovePost(w http.ResponseWriter, r *http.Request) {
	p, m, ok := s.getProjectAndMember(w, r)
	if !ok {
		return
	}

	targetUIDStr := r.PathValue("uid")
	targetUID, err := strconv.ParseInt(targetUIDStr, 10, 64)
	if err != nil {
		redirectWithError(w, r, fmt.Sprintf("/projects/%d/members", p.ID), "用户ID格式无效")
		return
	}

	if err := project.RemoveMember(s.db, p.ID, m.UserID, targetUID); err != nil {
		redirectWithError(w, r, fmt.Sprintf("/projects/%d/members", p.ID), err.Error())
		return
	}

	redirectWithSuccess(w, r, fmt.Sprintf("/projects/%d/members", p.ID), "成员已移出项目")
}

func (s *Server) handleProjectLeavePost(w http.ResponseWriter, r *http.Request) {
	p, m, ok := s.getProjectAndMember(w, r)
	if !ok {
		return
	}

	if err := project.RemoveMember(s.db, p.ID, m.UserID, m.UserID); err != nil {
		redirectWithError(w, r, fmt.Sprintf("/projects/%d/members", p.ID), err.Error())
		return
	}

	redirectWithSuccess(w, r, "/", "你已退出项目 "+p.Name)
}

func (s *Server) handleProjectSettingsGet(w http.ResponseWriter, r *http.Request) {
	p, m, ok := s.getProjectAndMember(w, r)
	if !ok {
		return
	}

	if m.Role != "owner" {
		s.RenderError(w, http.StatusNotFound, "页面未找到", "该页面不存在或你无权访问")
		return
	}

	ctx := s.newViewContext(r, p.Name+" · 设置", "projects")
	ctx["Project"] = p
	ctx["CurrentMember"] = m
	ctx["PaletteColors"] = project.PaletteColors

	if err := s.renderer.Render(w, "project_settings", ctx); err != nil {
		s.RenderError(w, http.StatusInternalServerError, "渲染设置页面失败", err.Error())
	}
}

func (s *Server) handleProjectUpdatePost(w http.ResponseWriter, r *http.Request) {
	p, m, ok := s.getProjectAndMember(w, r)
	if !ok {
		return
	}

	if m.Role != "owner" {
		s.RenderError(w, http.StatusNotFound, "页面未找到", "该页面不存在或你无权访问")
		return
	}

	if err := r.ParseForm(); err != nil {
		redirectWithError(w, r, fmt.Sprintf("/projects/%d/settings", p.ID), "表单解析失败")
		return
	}

	name := r.FormValue("name")
	description := r.FormValue("description")
	color := r.FormValue("color")

	if err := project.UpdateProject(s.db, p.ID, m.UserID, name, description, color); err != nil {
		redirectWithError(w, r, fmt.Sprintf("/projects/%d/settings", p.ID), err.Error())
		return
	}

	redirectWithSuccess(w, r, fmt.Sprintf("/projects/%d/settings", p.ID), "项目设置已保存")
}

func (s *Server) handleProjectInviteResetPost(w http.ResponseWriter, r *http.Request) {
	p, m, ok := s.getProjectAndMember(w, r)
	if !ok {
		return
	}

	if m.Role != "owner" {
		s.RenderError(w, http.StatusNotFound, "页面未找到", "该页面不存在或你无权访问")
		return
	}

	newCode, err := project.ResetInviteCode(s.db, p.ID, m.UserID)
	if err != nil {
		redirectWithError(w, r, fmt.Sprintf("/projects/%d/members", p.ID), err.Error())
		return
	}

	redirectWithSuccess(w, r, fmt.Sprintf("/projects/%d/members", p.ID), "邀请码已重置为: "+newCode)
}

func (s *Server) handleProjectDeletePost(w http.ResponseWriter, r *http.Request) {
	p, m, ok := s.getProjectAndMember(w, r)
	if !ok {
		return
	}

	if m.Role != "owner" {
		s.RenderError(w, http.StatusNotFound, "页面未找到", "该页面不存在或你无权访问")
		return
	}

	if err := project.DeleteProject(s.db, p.ID, m.UserID); err != nil {
		redirectWithError(w, r, fmt.Sprintf("/projects/%d/settings", p.ID), err.Error())
		return
	}

	redirectWithSuccess(w, r, "/", "项目已彻底删除")
}
