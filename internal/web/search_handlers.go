package web

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/luvsagiri/nocturnedesk/internal/auth"
	"github.com/luvsagiri/nocturnedesk/internal/project"
	"github.com/luvsagiri/nocturnedesk/internal/search"
)

func (s *Server) handleSearchGet(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFromContext(r.Context())
	if u == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// 检查是否指定了项目过滤
	pidStr := r.PathValue("pid")
	if pidStr == "" {
		pidStr = r.URL.Query().Get("pid")
	}
	if pidStr == "" {
		pidStr = r.URL.Query().Get("project_id")
	}

	var targetPID int64
	if pidStr != "" {
		var err error
		targetPID, err = strconv.ParseInt(pidStr, 10, 64)
		if err != nil {
			s.RenderError(w, http.StatusNotFound, "项目未找到", "项目ID不存在")
			return
		}

		// 可见性铁律：必须是该项目成员，否则 404！
		_, _, err = project.GetProjectDetails(s.db, targetPID, u.ID)
		if err != nil {
			s.RenderError(w, http.StatusNotFound, "项目未找到", "该项目不存在或你无权访问")
			return
		}
	}

	q := strings.TrimSpace(r.URL.Query().Get("q"))
	ctx := s.newViewContext(r, "全站搜索", "search")
	ctx["Query"] = q

	var results []search.Result
	if q != "" {
		var err error
		results, err = search.PerformSearch(s.db, u.ID, targetPID, q)
		if err != nil {
			s.RenderError(w, http.StatusInternalServerError, "检索失败", err.Error())
			return
		}
	}
	ctx["Results"] = results

	if err := s.renderer.Render(w, "search", ctx); err != nil {
		s.RenderError(w, http.StatusInternalServerError, "渲染搜索页面失败", err.Error())
	}
}
