package item

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/luvsagiri/nocturnedesk/internal/db"
)

var (
	ErrFolderNotFound    = errors.New("文件夹不存在")
	ErrItemNotFound      = errors.New("条目不存在")
	ErrInvalidFolderName = errors.New("文件夹名称不能为空且不超过 64 个字符")
	ErrInvalidDocTitle   = errors.New("文档标题不能为空且不超过 128 个字符")
	ErrCircularMove      = errors.New("不能将文件夹移动到自身或其子文件夹内")
)

type Folder struct {
	ID        int64  `json:"id"`
	ProjectID int64  `json:"project_id"`
	ParentID  *int64 `json:"parent_id"`
	Name      string `json:"name"`
	Position  int    `json:"position"`
	CreatedAt string `json:"created_at"`
}

type Item struct {
	ID           int64   `json:"id"`
	ProjectID    int64   `json:"project_id"`
	FolderID     *int64  `json:"folder_id"`
	Kind         string  `json:"kind"` // 'doc' or 'file'
	Title        string  `json:"title"`
	BodyHTML     *string `json:"body_html"`
	StorageKey   *string `json:"storage_key"`
	OriginalName *string `json:"original_name"`
	Mime         *string `json:"mime"`
	SizeBytes    *int64  `json:"size_bytes"`
	CreatedBy    int64   `json:"created_by"`
	CreatorName  string  `json:"creator_name"`
	CreatorColor string  `json:"creator_color"`
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`
}

func (i *Item) IsImage() bool {
	if i.Mime != nil && strings.HasPrefix(*i.Mime, "image/") {
		return true
	}
	if i.OriginalName != nil {
		ext := strings.ToLower(filepath.Ext(*i.OriginalName))
		return ext == ".jpg" || ext == ".jpeg" || ext == ".png" || ext == ".gif" || ext == ".webp" || ext == ".svg"
	}
	return false
}

func (i *Item) IsPDF() bool {
	if i.Mime != nil && *i.Mime == "application/pdf" {
		return true
	}
	if i.OriginalName != nil && strings.ToLower(filepath.Ext(*i.OriginalName)) == ".pdf" {
		return true
	}
	return false
}

func (i *Item) IsVideo() bool {
	if i.Mime != nil && strings.HasPrefix(*i.Mime, "video/") {
		return true
	}
	if i.OriginalName != nil {
		ext := strings.ToLower(filepath.Ext(*i.OriginalName))
		return ext == ".mp4" || ext == ".webm"
	}
	return false
}

func (i *Item) IsAudio() bool {
	if i.Mime != nil && strings.HasPrefix(*i.Mime, "audio/") {
		return true
	}
	if i.OriginalName != nil {
		ext := strings.ToLower(filepath.Ext(*i.OriginalName))
		return ext == ".mp3" || ext == ".wav" || ext == ".m4a"
	}
	return false
}

func (i *Item) IsTextOrMarkdown() bool {
	if i.Kind == "doc" {
		return true
	}
	if i.OriginalName != nil {
		ext := strings.ToLower(filepath.Ext(*i.OriginalName))
		switch ext {
		case ".md", ".markdown", ".txt", ".json", ".yaml", ".yml", ".log", ".csv", ".sh", ".sql", ".go", ".py", ".js", ".ts", ".html", ".css", ".xml":
			return true
		}
	}
	if i.Mime != nil && (strings.HasPrefix(*i.Mime, "text/") || strings.Contains(*i.Mime, "markdown") || strings.Contains(*i.Mime, "json")) {
		return true
	}
	return false
}

func (i *Item) IsMarkdown() bool {
	if i.Kind == "doc" {
		return true
	}
	if i.OriginalName != nil {
		ext := strings.ToLower(filepath.Ext(*i.OriginalName))
		return ext == ".md" || ext == ".markdown"
	}
	return false
}

func CreateFolder(database *db.DB, projectID int64, parentID *int64, name string) (*Folder, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 64 {
		return nil, ErrInvalidFolderName
	}

	var parentVal sql.NullInt64
	if parentID != nil {
		parentVal = sql.NullInt64{Int64: *parentID, Valid: true}
	}

	now := time.Now().UTC().Format(time.RFC3339)
	res, err := database.Exec(`
		INSERT INTO folders (project_id, parent_id, name, position, created_at)
		VALUES (?, ?, ?, 0, ?)
	`, projectID, parentVal, name, now)
	if err != nil {
		return nil, fmt.Errorf("failed to create folder: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}

	return &Folder{
		ID:        id,
		ProjectID: projectID,
		ParentID:  parentID,
		Name:      name,
		Position:  0,
		CreatedAt: now,
	}, nil
}

func RenameFolder(database *db.DB, folderID int64, newName string) error {
	newName = strings.TrimSpace(newName)
	if newName == "" || len(newName) > 64 {
		return ErrInvalidFolderName
	}
	_, err := database.Exec("UPDATE folders SET name = ? WHERE id = ?", newName, folderID)
	return err
}

func MoveFolder(database *db.DB, folderID int64, targetParentID *int64) error {
	// 循环依赖检查：不能移入自身
	if targetParentID != nil && *targetParentID == folderID {
		return ErrCircularMove
	}

	// 检查 targetParentID 是否是 folderID 的子目录
	curr := targetParentID
	for curr != nil {
		if *curr == folderID {
			return ErrCircularMove
		}
		var pID sql.NullInt64
		err := database.QueryRow("SELECT parent_id FROM folders WHERE id = ?", *curr).Scan(&pID)
		if err != nil || !pID.Valid {
			break
		}
		curr = &pID.Int64
	}

	var parentVal sql.NullInt64
	if targetParentID != nil {
		parentVal = sql.NullInt64{Int64: *targetParentID, Valid: true}
	}

	_, err := database.Exec("UPDATE folders SET parent_id = ? WHERE id = ?", parentVal, folderID)
	return err
}

func DeleteFolder(database *db.DB, folderID int64, dataDir string) error {
	// 查询该文件夹及其子目录下所有 item 的物理存储文件并清理
	rows, err := database.Query(`
		WITH RECURSIVE subfolders(id) AS (
			SELECT id FROM folders WHERE id = ?
			UNION ALL
			SELECT f.id FROM folders f JOIN subfolders sf ON f.parent_id = sf.id
		)
		SELECT storage_key FROM items WHERE folder_id IN (SELECT id FROM subfolders) AND storage_key IS NOT NULL
	`, folderID)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var k string
			if err := rows.Scan(&k); err == nil && k != "" {
				_ = os.Remove(filepath.Join(dataDir, k))
			}
		}
	}

	_, err = database.Exec("DELETE FROM folders WHERE id = ?", folderID)
	return err
}

func GetFolder(database *db.DB, folderID int64) (*Folder, error) {
	var f Folder
	var pID sql.NullInt64
	err := database.QueryRow(`
		SELECT id, project_id, parent_id, name, position, created_at
		FROM folders
		WHERE id = ?
	`, folderID).Scan(&f.ID, &f.ProjectID, &pID, &f.Name, &f.Position, &f.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrFolderNotFound
		}
		return nil, err
	}
	if pID.Valid {
		f.ParentID = &pID.Int64
	}
	return &f, nil
}

func ListFoldersInProject(database *db.DB, projectID int64) ([]Folder, error) {
	rows, err := database.Query(`
		SELECT id, project_id, parent_id, name, position, created_at
		FROM folders
		WHERE project_id = ?
		ORDER BY parent_id IS NOT NULL, position ASC, name ASC
	`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var folders []Folder
	for rows.Next() {
		var f Folder
		var pID sql.NullInt64
		if err := rows.Scan(&f.ID, &f.ProjectID, &pID, &f.Name, &f.Position, &f.CreatedAt); err != nil {
			return nil, err
		}
		if pID.Valid {
			f.ParentID = &pID.Int64
		}
		folders = append(folders, f)
	}
	return folders, nil
}

func ListSubfolders(database *db.DB, projectID int64, parentID *int64) ([]Folder, error) {
	var query string
	var args []any
	if parentID == nil {
		query = "SELECT id, project_id, parent_id, name, position, created_at FROM folders WHERE project_id = ? AND parent_id IS NULL ORDER BY name ASC"
		args = []any{projectID}
	} else {
		query = "SELECT id, project_id, parent_id, name, position, created_at FROM folders WHERE project_id = ? AND parent_id = ? ORDER BY name ASC"
		args = []any{projectID, *parentID}
	}

	rows, err := database.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []Folder
	for rows.Next() {
		var f Folder
		var pID sql.NullInt64
		if err := rows.Scan(&f.ID, &f.ProjectID, &pID, &f.Name, &f.Position, &f.CreatedAt); err != nil {
			return nil, err
		}
		if pID.Valid {
			f.ParentID = &pID.Int64
		}
		list = append(list, f)
	}
	return list, nil
}

func GetBreadcrumbs(database *db.DB, folderID *int64) ([]Folder, error) {
	if folderID == nil {
		return nil, nil
	}
	var chain []Folder
	currID := folderID
	for currID != nil {
		f, err := GetFolder(database, *currID)
		if err != nil {
			break
		}
		chain = append([]Folder{*f}, chain...) // prepend
		currID = f.ParentID
	}
	return chain, nil
}

func ListItems(database *db.DB, projectID int64, folderID *int64, kindFilter string) ([]Item, error) {
	var folderClause string
	args := []any{projectID}
	if folderID == nil {
		folderClause = "AND i.folder_id IS NULL"
	} else {
		folderClause = "AND i.folder_id = ?"
		args = append(args, *folderID)
	}

	var kindClause string
	if kindFilter == "doc" {
		kindClause = "AND (i.kind = 'doc' OR (i.kind = 'file' AND (i.original_name LIKE '%.md' OR i.original_name LIKE '%.markdown' OR i.original_name LIKE '%.txt' OR i.original_name LIKE '%.json' OR i.original_name LIKE '%.yaml' OR i.original_name LIKE '%.yml' OR i.original_name LIKE '%.log')))"
	} else if kindFilter == "file" {
		kindClause = "AND i.kind = 'file' AND NOT (i.original_name LIKE '%.md' OR i.original_name LIKE '%.markdown' OR i.original_name LIKE '%.txt')"
	}

	query := fmt.Sprintf(`
		SELECT i.id, i.project_id, i.folder_id, i.kind, i.title, i.body_html, i.storage_key,
		       i.original_name, i.mime, i.size_bytes, i.created_by,
		       u.display_name, pm.color, i.created_at, i.updated_at
		FROM items i
		JOIN users u ON i.created_by = u.id
		JOIN project_members pm ON (i.project_id = pm.project_id AND i.created_by = pm.user_id)
		WHERE i.project_id = ? %s %s
		ORDER BY i.updated_at DESC
	`, folderClause, kindClause)

	rows, err := database.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query items: %w", err)
	}
	defer rows.Close()

	var items []Item
	for rows.Next() {
		var it Item
		var fid sql.NullInt64
		var body, sKey, oName, mime sql.NullString
		var size sql.NullInt64
		if err := rows.Scan(
			&it.ID, &it.ProjectID, &fid, &it.Kind, &it.Title, &body, &sKey,
			&oName, &mime, &size, &it.CreatedBy,
			&it.CreatorName, &it.CreatorColor, &it.CreatedAt, &it.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if fid.Valid {
			it.FolderID = &fid.Int64
		}
		if body.Valid {
			it.BodyHTML = &body.String
		}
		if sKey.Valid {
			it.StorageKey = &sKey.String
		}
		if oName.Valid {
			it.OriginalName = &oName.String
		}
		if mime.Valid {
			it.Mime = &mime.String
		}
		if size.Valid {
			it.SizeBytes = &size.Int64
		}
		items = append(items, it)
	}
	return items, nil
}

func GetItem(database *db.DB, itemID int64) (*Item, error) {
	row := database.QueryRow(`
		SELECT i.id, i.project_id, i.folder_id, i.kind, i.title, i.body_html, i.storage_key,
		       i.original_name, i.mime, i.size_bytes, i.created_by,
		       u.display_name, pm.color, i.created_at, i.updated_at
		FROM items i
		JOIN users u ON i.created_by = u.id
		JOIN project_members pm ON (i.project_id = pm.project_id AND i.created_by = pm.user_id)
		WHERE i.id = ?
	`, itemID)

	var it Item
	var fid sql.NullInt64
	var body, sKey, oName, mime sql.NullString
	var size sql.NullInt64
	if err := row.Scan(
		&it.ID, &it.ProjectID, &fid, &it.Kind, &it.Title, &body, &sKey,
		&oName, &mime, &size, &it.CreatedBy,
		&it.CreatorName, &it.CreatorColor, &it.CreatedAt, &it.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrItemNotFound
		}
		return nil, err
	}
	if fid.Valid {
		it.FolderID = &fid.Int64
	}
	if body.Valid {
		it.BodyHTML = &body.String
	}
	if sKey.Valid {
		it.StorageKey = &sKey.String
	}
	if oName.Valid {
		it.OriginalName = &oName.String
	}
	if mime.Valid {
		it.Mime = &mime.String
	}
	if size.Valid {
		it.SizeBytes = &size.Int64
	}
	return &it, nil
}

func CreateDoc(database *db.DB, projectID int64, folderID *int64, userID int64, title, bodyHTML string) (*Item, error) {
	title = strings.TrimSpace(title)
	if title == "" || len(title) > 128 {
		return nil, ErrInvalidDocTitle
	}

	var fidVal sql.NullInt64
	if folderID != nil {
		fidVal = sql.NullInt64{Int64: *folderID, Valid: true}
	}

	now := time.Now().UTC().Format(time.RFC3339)
	res, err := database.Exec(`
		INSERT INTO items (project_id, folder_id, kind, title, body_html, storage_key, original_name, mime, size_bytes, created_by, created_at, updated_at)
		VALUES (?, ?, 'doc', ?, ?, NULL, NULL, 'text/html', ?, ?, ?, ?)
	`, projectID, fidVal, title, bodyHTML, len(bodyHTML), userID, now, now)
	if err != nil {
		return nil, fmt.Errorf("failed to create doc: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}

	// 同步到全站搜索 search_index
	_, _ = database.Exec(`
		INSERT INTO search_index (title, body, project_id, kind, ref_id)
		VALUES (?, ?, ?, 'doc', ?)
	`, title, bodyHTML, projectID, id)

	return GetItem(database, id)
}

func UpdateDoc(database *db.DB, itemID int64, title, bodyHTML string, folderID *int64) error {
	title = strings.TrimSpace(title)
	if title == "" || len(title) > 128 {
		return ErrInvalidDocTitle
	}

	var fidVal sql.NullInt64
	if folderID != nil {
		fidVal = sql.NullInt64{Int64: *folderID, Valid: true}
	}

	now := time.Now().UTC().Format(time.RFC3339)
	_, err := database.Exec(`
		UPDATE items
		SET title = ?, body_html = ?, folder_id = ?, size_bytes = ?, updated_at = ?
		WHERE id = ?
	`, title, bodyHTML, fidVal, len(bodyHTML), now, itemID)
	if err != nil {
		return fmt.Errorf("failed to update doc: %w", err)
	}

	// 同步 search_index
	_, _ = database.Exec(`DELETE FROM search_index WHERE kind = 'doc' AND ref_id = ?`, itemID)
	var pid int64
	_ = database.QueryRow("SELECT project_id FROM items WHERE id = ?", itemID).Scan(&pid)
	_, _ = database.Exec(`
		INSERT INTO search_index (title, body, project_id, kind, ref_id)
		VALUES (?, ?, ?, 'doc', ?)
	`, title, bodyHTML, pid, itemID)

	return nil
}

func CreateFile(database *db.DB, projectID int64, folderID *int64, userID int64, originalName, mime string, sizeBytes int64, storageKey string) (*Item, error) {
	originalName = strings.TrimSpace(originalName)
	if originalName == "" {
		originalName = "unnamed"
	}

	var fidVal sql.NullInt64
	if folderID != nil {
		fidVal = sql.NullInt64{Int64: *folderID, Valid: true}
	}

	now := time.Now().UTC().Format(time.RFC3339)
	res, err := database.Exec(`
		INSERT INTO items (project_id, folder_id, kind, title, body_html, storage_key, original_name, mime, size_bytes, created_by, created_at, updated_at)
		VALUES (?, ?, 'file', ?, NULL, ?, ?, ?, ?, ?, ?, ?)
	`, projectID, fidVal, originalName, storageKey, originalName, mime, sizeBytes, userID, now, now)
	if err != nil {
		return nil, fmt.Errorf("failed to create file item: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}

	// 同步到全站搜索 search_index
	_, _ = database.Exec(`
		INSERT INTO search_index (title, body, project_id, kind, ref_id)
		VALUES (?, ?, ?, 'file', ?)
	`, originalName, originalName, projectID, id)

	return GetItem(database, id)
}

func UpdateFileText(database *db.DB, itemID int64, title, content string, folderID *int64, dataDir string) error {
	title = strings.TrimSpace(title)
	if title == "" || len(title) > 128 {
		return ErrInvalidDocTitle
	}

	it, err := GetItem(database, itemID)
	if err != nil {
		return err
	}

	if it.StorageKey != nil && *it.StorageKey != "" {
		filePath := filepath.Join(dataDir, *it.StorageKey)
		if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
			return fmt.Errorf("failed to write updated file content: %w", err)
		}
	}

	var fidVal sql.NullInt64
	if folderID != nil {
		fidVal = sql.NullInt64{Int64: *folderID, Valid: true}
	} else if it.FolderID != nil {
		fidVal = sql.NullInt64{Int64: *it.FolderID, Valid: true}
	}

	now := time.Now().UTC().Format(time.RFC3339)
	newSize := int64(len(content))

	_, err = database.Exec(`
		UPDATE items
		SET title = ?, size_bytes = ?, folder_id = ?, updated_at = ?
		WHERE id = ?
	`, title, newSize, fidVal, now, itemID)
	if err != nil {
		return fmt.Errorf("failed to update file item: %w", err)
	}

	// 同步 search_index
	_, _ = database.Exec(`DELETE FROM search_index WHERE ref_id = ?`, itemID)
	_, _ = database.Exec(`
		INSERT INTO search_index (title, body, project_id, kind, ref_id)
		VALUES (?, ?, ?, 'file', ?)
	`, title, content, it.ProjectID, itemID)

	return nil
}

func DeleteItem(database *db.DB, itemID int64, dataDir string) error {
	var storageKey sql.NullString
	_ = database.QueryRow("SELECT storage_key FROM items WHERE id = ?", itemID).Scan(&storageKey)
	if storageKey.Valid && storageKey.String != "" {
		_ = os.Remove(filepath.Join(dataDir, storageKey.String))
	}

	_, err := database.Exec("DELETE FROM items WHERE id = ?", itemID)
	if err != nil {
		return err
	}

	_, _ = database.Exec("DELETE FROM search_index WHERE ref_id = ?", itemID)
	return nil
}
