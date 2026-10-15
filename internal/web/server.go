package web

import (
	"io/fs"
	"log"
	"net/http"

	"github.com/luvsagiri/nocturnedesk/internal/auth"
	"github.com/luvsagiri/nocturnedesk/internal/config"
	"github.com/luvsagiri/nocturnedesk/internal/db"
	"github.com/luvsagiri/nocturnedesk/internal/project"
)

type Server struct {
	cfg      *config.Config
	db       *db.DB
	renderer *TemplateRenderer
	mux      *http.ServeMux
	staticFS fs.FS
}

func NewServer(cfg *config.Config, database *db.DB, templatesFS fs.FS, staticFS fs.FS) *Server {
	renderer := NewTemplateRenderer(templatesFS)
	s := &Server{
		cfg:      cfg,
		db:       database,
		renderer: renderer,
		mux:      http.NewServeMux(),
		staticFS: staticFS,
	}

	s.routes()
	return s
}

func (s *Server) routes() {
	// 静态资源服务：/static/
	staticSub, err := fs.Sub(s.staticFS, "static")
	if err != nil {
		log.Printf("Warning: failed to get static sub fs: %v", err)
		s.mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(s.staticFS))))
	} else {
		s.mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticSub))))
	}

	// 存活探测探针
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	// 认证与账号
	s.mux.HandleFunc("GET /login", s.handleLoginGet)
	s.mux.HandleFunc("POST /login", s.handleLoginPost)
	s.mux.HandleFunc("GET /register", s.handleRegisterGet)
	s.mux.HandleFunc("POST /register", s.handleRegisterPost)
	s.mux.HandleFunc("POST /logout", s.handleLogoutPost)

	// 个人中心 (我的)
	s.mux.HandleFunc("GET /me", s.requireAuth(s.handleMeGet))
	s.mux.HandleFunc("POST /me/profile", s.requireAuth(s.handleMeProfilePost))
	s.mux.HandleFunc("POST /me/password", s.requireAuth(s.handleMePasswordPost))
	s.mux.HandleFunc("GET /files/avatar", s.handleAvatarFileGet)

	// 首页 (未登录重定向至 /login)
	s.mux.HandleFunc("GET /{$}", s.requireAuth(s.handleHomeGet))

	// 项目与成员
	s.mux.HandleFunc("GET /projects/new", s.requireAuth(s.handleProjectNewGet))
	s.mux.HandleFunc("POST /projects", s.requireAuth(s.handleProjectCreatePost))
	s.mux.HandleFunc("POST /projects/join", s.requireAuth(s.handleProjectJoinPost))
	s.mux.HandleFunc("GET /projects/{pid}", s.requireAuth(s.handleProjectRootGet))
	s.mux.HandleFunc("GET /projects/{pid}/files", s.requireAuth(s.handleProjectFilesGet))
	s.mux.HandleFunc("GET /projects/{pid}/members", s.requireAuth(s.handleProjectMembersGet))
	s.mux.HandleFunc("POST /projects/{pid}/members/color", s.requireAuth(s.handleProjectMemberColorPost))
	s.mux.HandleFunc("POST /projects/{pid}/members/{uid}/remove", s.requireAuth(s.handleProjectMemberRemovePost))
	s.mux.HandleFunc("POST /projects/{pid}/leave", s.requireAuth(s.handleProjectLeavePost))
	s.mux.HandleFunc("GET /projects/{pid}/settings", s.requireAuth(s.handleProjectSettingsGet))
	s.mux.HandleFunc("POST /projects/{pid}/update", s.requireAuth(s.handleProjectUpdatePost))
	s.mux.HandleFunc("POST /projects/{pid}/invite/reset", s.requireAuth(s.handleProjectInviteResetPost))
	s.mux.HandleFunc("POST /projects/{pid}/delete", s.requireAuth(s.handleProjectDeletePost))

	// 文件夹与内容
	s.mux.HandleFunc("POST /projects/{pid}/folders", s.requireAuth(s.handleFolderCreatePost))
	s.mux.HandleFunc("POST /folders/{fid}/rename", s.requireAuth(s.handleFolderRenamePost))
	s.mux.HandleFunc("POST /folders/{fid}/move", s.requireAuth(s.handleFolderMovePost))
	s.mux.HandleFunc("POST /folders/{fid}/delete", s.requireAuth(s.handleFolderDeletePost))

	// 文档
	s.mux.HandleFunc("GET /projects/{pid}/docs/new", s.requireAuth(s.handleDocNewGet))
	s.mux.HandleFunc("POST /projects/{pid}/docs", s.requireAuth(s.handleDocCreatePost))
	s.mux.HandleFunc("GET /projects/{pid}/docs/{iid}", s.requireAuth(s.handleDocViewGet))
	s.mux.HandleFunc("GET /projects/{pid}/docs/{iid}/edit", s.requireAuth(s.handleDocEditGet))
	s.mux.HandleFunc("GET /docs/{iid}/download", s.requireAuth(s.handleDocDownloadGet))
	s.mux.HandleFunc("GET /items/{iid}/download", s.requireAuth(s.handleDocDownloadGet))
	s.mux.HandleFunc("POST /items/{iid}/update", s.requireAuth(s.handleItemUpdatePost))
	s.mux.HandleFunc("POST /items/{iid}/delete", s.requireAuth(s.handleItemDeletePost))

	// 资源上传
	s.mux.HandleFunc("POST /projects/{pid}/upload", s.requireAuth(s.handleFileUploadPost))

	// 日历
	s.mux.HandleFunc("GET /projects/{pid}/calendar", s.requireAuth(s.handleProjectCalendarGet))
	s.mux.HandleFunc("POST /projects/{pid}/calendar", s.requireAuth(s.handleProjectCalendarPost))

	// 看板与卡片
	s.mux.HandleFunc("GET /projects/{pid}/board", s.requireAuth(s.handleProjectBoardGet))
	s.mux.HandleFunc("POST /projects/{pid}/cards", s.requireAuth(s.handleCardCreatePost))
	s.mux.HandleFunc("POST /cards/{cid}/update", s.requireAuth(s.handleCardUpdatePost))
	s.mux.HandleFunc("POST /cards/{cid}/move", s.requireAuth(s.handleCardMovePost))
	s.mux.HandleFunc("POST /cards/{cid}/delete", s.requireAuth(s.handleCardDeletePost))

	// 标签管理
	s.mux.HandleFunc("GET /projects/{pid}/labels", s.requireAuth(s.handleProjectLabelsGet))
	s.mux.HandleFunc("POST /projects/{pid}/labels", s.requireAuth(s.handleProjectLabelCreatePost))
	s.mux.HandleFunc("POST /labels/{lid}/update", s.requireAuth(s.handleLabelUpdatePost))
	s.mux.HandleFunc("POST /labels/{lid}/delete", s.requireAuth(s.handleLabelDeletePost))

	// 文件下载/预览校验（可见性检查）
	s.mux.HandleFunc("GET /files/{iid}", s.requireAuth(s.handleFileDownloadGet))

	// 全站搜索与项目内搜索
	s.mux.HandleFunc("GET /search", s.requireAuth(s.handleSearchGet))
	s.mux.HandleFunc("GET /projects/{pid}/search", s.requireAuth(s.handleSearchGet))
}

func (s *Server) handleHomeGet(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFromContext(r.Context())
	if u == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	created, joined, err := project.GetProjectsForUser(s.db, u.ID)
	if err != nil {
		s.RenderError(w, http.StatusInternalServerError, "获取项目列表失败", err.Error())
		return
	}

	ctx := s.newViewContext(r, "项目", "projects")
	ctx["CreatedProjects"] = created
	ctx["JoinedProjects"] = joined

	if err := s.renderer.Render(w, "home", ctx); err != nil {
		s.RenderError(w, http.StatusInternalServerError, "渲染首页失败", err.Error())
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.sessionMiddleware(s.mux).ServeHTTP(w, r)
}

func (s *Server) Handler() http.Handler {
	return s.sessionMiddleware(s.mux)
}

func (s *Server) Mux() *http.ServeMux {
	return s.mux
}

func (s *Server) DB() *db.DB {
	return s.db
}

func (s *Server) Config() *config.Config {
	return s.cfg
}

func (s *Server) Renderer() *TemplateRenderer {
	return s.renderer
}

func (s *Server) RenderError(w http.ResponseWriter, code int, msg, detail string) {
	w.WriteHeader(code)
	data := map[string]any{
		"Code":    code,
		"Message": msg,
		"Detail":  detail,
		"Title":   msg,
	}
	if err := s.renderer.Render(w, "error", data); err != nil {
		http.Error(w, msg, code)
	}
}
