package web

import (
	"net/http"
	"net/url"

	"github.com/luvsagiri/nocturnedesk/internal/auth"
)

type ViewContext struct {
	Title        string
	ActiveNav    string
	CurrentUser  *auth.User
	FlashError   string
	FlashSuccess string
	SearchQuery  string
	Data         any
}

func (s *Server) newViewContext(r *http.Request, title string, activeNav string) map[string]any {
	u := auth.UserFromContext(r.Context())

	flashErr := r.URL.Query().Get("error")
	flashSucc := r.URL.Query().Get("success")

	ctx := map[string]any{
		"Title":        title,
		"ActiveNav":    activeNav,
		"CurrentUser":  u,
		"FlashError":   flashErr,
		"FlashSuccess": flashSucc,
		"SearchQuery":  r.URL.Query().Get("q"),
	}
	return ctx
}

func (s *Server) sessionMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(auth.SessionCookieName)
		if err == nil && cookie.Value != "" {
			userID, err := auth.VerifySessionToken(cookie.Value, s.cfg.SecretKey)
			if err == nil {
				user, err := auth.GetUserByID(s.db, userID)
				if err == nil && user != nil {
					ctx := auth.ContextWithUser(r.Context(), user)
					r = r.WithContext(ctx)
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := auth.UserFromContext(r.Context())
		if u == nil {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		next(w, r)
	}
}

func redirectWithError(w http.ResponseWriter, r *http.Request, targetURL, errMsg string) {
	u, _ := url.Parse(targetURL)
	q := u.Query()
	q.Set("error", errMsg)
	u.RawQuery = q.Encode()
	http.Redirect(w, r, u.String(), http.StatusSeeOther)
}

func redirectWithSuccess(w http.ResponseWriter, r *http.Request, targetURL, succMsg string) {
	u, _ := url.Parse(targetURL)
	q := u.Query()
	q.Set("success", succMsg)
	u.RawQuery = q.Encode()
	http.Redirect(w, r, u.String(), http.StatusSeeOther)
}
