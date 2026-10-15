package web

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/luvsagiri/nocturnedesk/internal/auth"
	"github.com/luvsagiri/nocturnedesk/internal/calendar"
)

func (s *Server) handleProjectCalendarGet(w http.ResponseWriter, r *http.Request) {
	p, m, ok := s.getProjectAndMember(w, r)
	if !ok {
		return
	}
	u := auth.UserFromContext(r.Context())

	monthParam := strings.TrimSpace(r.URL.Query().Get("month"))
	dayParam := strings.TrimSpace(r.URL.Query().Get("day"))

	monthView, err := calendar.BuildMonthView(s.db, p.ID, u.ID, monthParam, dayParam)
	if err != nil {
		s.RenderError(w, http.StatusInternalServerError, "获取日历视图失败", err.Error())
		return
	}

	ctx := s.newViewContext(r, p.Name+" · 日历", "projects")
	ctx["Project"] = p
	ctx["CurrentMember"] = m
	ctx["MonthView"] = monthView
	ctx["ActiveTab"] = "calendar"

	if err := s.renderer.Render(w, "project_calendar", ctx); err != nil {
		s.RenderError(w, http.StatusInternalServerError, "渲染日历页面失败", err.Error())
	}
}

func (s *Server) handleProjectCalendarPost(w http.ResponseWriter, r *http.Request) {
	p, _, ok := s.getProjectAndMember(w, r)
	if !ok {
		return
	}
	u := auth.UserFromContext(r.Context())

	if err := r.ParseForm(); err != nil {
		redirectWithError(w, r, fmt.Sprintf("/projects/%d/calendar", p.ID), "表单解析失败")
		return
	}

	day := r.FormValue("day")
	month := r.FormValue("month")
	content := r.FormValue("content")

	if err := calendar.UpsertEntry(s.db, p.ID, u.ID, day, content); err != nil {
		redirectURL := fmt.Sprintf("/projects/%d/calendar?month=%s&day=%s", p.ID, month, day)
		redirectWithError(w, r, redirectURL, err.Error())
		return
	}

	redirectURL := fmt.Sprintf("/projects/%d/calendar?month=%s&day=%s", p.ID, month, day)
	redirectWithSuccess(w, r, redirectURL, "日历记录已保存")
}
