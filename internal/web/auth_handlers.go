package web

import (
	"net/http"

	"github.com/luvsagiri/nocturnedesk/internal/auth"
)

func (s *Server) handleLoginGet(w http.ResponseWriter, r *http.Request) {
	if auth.UserFromContext(r.Context()) != nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	ctx := s.newViewContext(r, "登录", "")
	if err := s.renderer.Render(w, "login", ctx); err != nil {
		s.RenderError(w, http.StatusInternalServerError, "渲染登录页失败", err.Error())
	}
}

func (s *Server) handleLoginPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.RenderError(w, http.StatusBadRequest, "表单解析失败", err.Error())
		return
	}

	username := r.FormValue("username")
	password := r.FormValue("password")

	user, err := auth.Authenticate(s.db, username, password)
	if err != nil {
		ctx := s.newViewContext(r, "登录", "")
		ctx["FlashError"] = err.Error()
		_ = s.renderer.Render(w, "login", ctx)
		return
	}

	token := auth.CreateSessionToken(user.ID, s.cfg.SessionDuration, s.cfg.SecretKey)
	auth.SetSessionCookie(w, token, int(s.cfg.SessionDuration.Seconds()))

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) handleRegisterGet(w http.ResponseWriter, r *http.Request) {
	if auth.UserFromContext(r.Context()) != nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	ctx := s.newViewContext(r, "注册账号", "")
	if err := s.renderer.Render(w, "register", ctx); err != nil {
		s.RenderError(w, http.StatusInternalServerError, "渲染注册页失败", err.Error())
	}
}

func (s *Server) handleRegisterPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.RenderError(w, http.StatusBadRequest, "表单解析失败", err.Error())
		return
	}

	username := r.FormValue("username")
	displayName := r.FormValue("display_name")
	password := r.FormValue("password")

	user, err := auth.Register(s.db, username, displayName, password)
	if err != nil {
		ctx := s.newViewContext(r, "注册账号", "")
		ctx["FlashError"] = err.Error()
		_ = s.renderer.Render(w, "register", ctx)
		return
	}

	token := auth.CreateSessionToken(user.ID, s.cfg.SessionDuration, s.cfg.SecretKey)
	auth.SetSessionCookie(w, token, int(s.cfg.SessionDuration.Seconds()))

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) handleLogoutPost(w http.ResponseWriter, r *http.Request) {
	auth.ClearSessionCookie(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
