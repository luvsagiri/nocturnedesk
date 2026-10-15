package search

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"github.com/luvsagiri/nocturnedesk/internal/db"
)

type Result struct {
	Kind        string `json:"kind"` // 'doc', 'file', 'calendar', 'card'
	RefID       int64  `json:"ref_id"`
	ProjectID   int64  `json:"project_id"`
	ProjectName string `json:"project_name"`
	Title       string `json:"title"`
	Snippet     string `json:"snippet"`
	Link        string `json:"link"`
}

func SyncIndex(database *db.DB, kind string, refID int64, projectID int64, title, body string) error {
	// 无论之前是否存在，先清理旧条目
	_, _ = database.Exec("DELETE FROM search_index WHERE kind = ? AND ref_id = ?", kind, refID)

	title = strings.TrimSpace(title)
	body = strings.TrimSpace(body)
	if title == "" && body == "" {
		return nil
	}

	_, err := database.Exec(`
		INSERT INTO search_index (title, body, project_id, kind, ref_id)
		VALUES (?, ?, ?, ?, ?)
	`, title, body, projectID, kind, refID)
	return err
}

func DeleteIndex(database *db.DB, kind string, refID int64) error {
	_, err := database.Exec("DELETE FROM search_index WHERE kind = ? AND ref_id = ?", kind, refID)
	return err
}

func DeleteProjectIndexes(database *db.DB, projectID int64) error {
	_, err := database.Exec("DELETE FROM search_index WHERE project_id = ?", projectID)
	return err
}

func PerformSearch(database *db.DB, userID int64, targetPID int64, query string) ([]Result, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}

	// 1. 优先尝试 FTS5 trigram 全文搜索
	ftsResults, err := searchFTS5(database, userID, targetPID, query)
	if err == nil && len(ftsResults) > 0 {
		return ftsResults, nil
	}

	// 2. FTS5 未命中或报错时，自动退化使用 LIKE 兜底检索
	return searchLIKE(database, userID, targetPID, query)
}

func searchFTS5(database *db.DB, userID int64, targetPID int64, query string) ([]Result, error) {
	// 为 trigram 构造精确短语匹配词（防止特殊字符注入损坏 FTS5 语法）
	cleanQuery := strings.ReplaceAll(query, `"`, `""`)
	matchExpr := fmt.Sprintf(`"%s"`, cleanQuery)

	sqlStr := `
		SELECT s.kind, s.ref_id, s.project_id, p.name, s.title,
		       snippet(search_index, 1, '<b>', '</b>', '...', 24)
		FROM search_index s
		JOIN projects p ON s.project_id = p.id
		JOIN project_members pm ON p.id = pm.project_id
		WHERE pm.user_id = ?
		  AND (? = 0 OR s.project_id = ?)
		  AND search_index MATCH ?
		ORDER BY rank
		LIMIT 50
	`
	rows, err := database.Query(sqlStr, userID, targetPID, targetPID, matchExpr)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []Result
	for rows.Next() {
		var r Result
		var snippet sql.NullString
		if err := rows.Scan(&r.Kind, &r.RefID, &r.ProjectID, &r.ProjectName, &r.Title, &snippet); err != nil {
			return nil, err
		}
		if snippet.Valid && snippet.String != "" {
			r.Snippet = snippet.String
		} else {
			r.Snippet = r.Title
		}
		r.Link = FormatLink(r.ProjectID, r.Kind, r.RefID)
		results = append(results, r)
	}
	return results, nil
}

func searchLIKE(database *db.DB, userID int64, targetPID int64, query string) ([]Result, error) {
	likeTerm := "%" + query + "%"
	var results []Result

	// (a) 检索 items (文档与资源)
	itemRows, err := database.Query(`
		SELECT i.kind, i.id, i.project_id, p.name, i.title, COALESCE(i.body_html, '')
		FROM items i
		JOIN projects p ON i.project_id = p.id
		JOIN project_members pm ON p.id = pm.project_id
		WHERE pm.user_id = ?
		  AND (? = 0 OR i.project_id = ?)
		  AND (i.title LIKE ? OR i.body_html LIKE ? OR i.original_name LIKE ?)
		LIMIT 20
	`, userID, targetPID, targetPID, likeTerm, likeTerm, likeTerm)
	if err == nil {
		defer itemRows.Close()
		for itemRows.Next() {
			var r Result
			var body string
			if err := itemRows.Scan(&r.Kind, &r.RefID, &r.ProjectID, &r.ProjectName, &r.Title, &body); err == nil {
				if len(body) > 100 {
					r.Snippet = body[:100] + "..."
				} else {
					r.Snippet = body
				}
				r.Link = FormatLink(r.ProjectID, r.Kind, r.RefID)
				results = append(results, r)
			}
		}
	}

	// (b) 检索 cards (看板卡片)
	cardRows, err := database.Query(`
		SELECT 'card', c.id, c.project_id, p.name, c.title, c.description
		FROM cards c
		JOIN projects p ON c.project_id = p.id
		JOIN project_members pm ON p.id = pm.project_id
		WHERE pm.user_id = ?
		  AND (? = 0 OR c.project_id = ?)
		  AND (c.title LIKE ? OR c.description LIKE ?)
		LIMIT 20
	`, userID, targetPID, targetPID, likeTerm, likeTerm)
	if err == nil {
		defer cardRows.Close()
		for cardRows.Next() {
			var r Result
			var desc string
			if err := cardRows.Scan(&r.Kind, &r.RefID, &r.ProjectID, &r.ProjectName, &r.Title, &desc); err == nil {
				r.Snippet = desc
				r.Link = FormatLink(r.ProjectID, r.Kind, r.RefID)
				results = append(results, r)
			}
		}
	}

	// (c) 检索 calendar_entries (日历纯文本)
	calRows, err := database.Query(`
		SELECT 'calendar', ce.id, ce.project_id, p.name, ce.day || ' 成员日历记录', ce.content
		FROM calendar_entries ce
		JOIN projects p ON ce.project_id = p.id
		JOIN project_members pm ON p.id = pm.project_id
		WHERE pm.user_id = ?
		  AND (? = 0 OR ce.project_id = ?)
		  AND ce.content LIKE ?
		LIMIT 20
	`, userID, targetPID, targetPID, likeTerm)
	if err == nil {
		defer calRows.Close()
		for calRows.Next() {
			var r Result
			var content string
			if err := calRows.Scan(&r.Kind, &r.RefID, &r.ProjectID, &r.ProjectName, &r.Title, &content); err == nil {
				r.Snippet = content
				r.Link = FormatLink(r.ProjectID, r.Kind, r.RefID)
				results = append(results, r)
			}
		}
	}

	return results, nil
}

func FormatLink(projectID int64, kind string, refID int64) string {
	switch kind {
	case "doc":
		return "/projects/" + strconv.FormatInt(projectID, 10) + "/docs/" + strconv.FormatInt(refID, 10)
	case "file":
		return "/files/" + strconv.FormatInt(refID, 10)
	case "calendar":
		return "/projects/" + strconv.FormatInt(projectID, 10) + "/calendar"
	case "card":
		return "/projects/" + strconv.FormatInt(projectID, 10) + "/board"
	default:
		return "/projects/" + strconv.FormatInt(projectID, 10) + "/files"
	}
}
