package project

import (
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/luvsagiri/nocturnedesk/internal/db"
)

var (
	ErrProjectNotFound    = errors.New("项目不存在")
	ErrNotMember          = errors.New("你不是该项目的成员")
	ErrNotOwner           = errors.New("只有项目创建者可以执行该操作")
	ErrOwnerCannotLeave   = errors.New("项目创建者不能退出项目，若不再需要请直接删除项目")
	ErrInvalidInviteCode  = errors.New("邀请码无效或已失效")
	ErrAlreadyMember      = errors.New("你已经是该项目的成员了")
	ErrInvalidProjectName = errors.New("项目名称不能为空且长度不超过 64 个字符")
)

// 可供成员选择的固定调色板
var PaletteColors = []string{
	"#6366f1", // Indigo
	"#3b82f6", // Blue
	"#06b6d4", // Cyan
	"#10b981", // Emerald
	"#84cc16", // Lime
	"#eab308", // Yellow
	"#f97316", // Orange
	"#ef4444", // Red
	"#ec4899", // Pink
	"#a855f7", // Purple
	"#64748b", // Slate
}

type Project struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	InviteCode  string `json:"invite_code"`
	Color       string `json:"color"`
	CreatedBy   int64  `json:"created_by"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
	MemberCount int    `json:"member_count"`
	Role        string `json:"role"`
}

type Member struct {
	ProjectID   int64   `json:"project_id"`
	UserID      int64   `json:"user_id"`
	Username    string  `json:"username"`
	DisplayName string  `json:"display_name"`
	AvatarKey   *string `json:"avatar_key"`
	Role        string  `json:"role"`
	Color       string  `json:"color"`
	JoinedAt    string  `json:"joined_at"`
}

// 生成 8 位无歧义随机短码（排除易混字符 0O1lI）
func GenerateInviteCode() string {
	const chars = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"
	b := make([]byte, 8)
	randomBytes := make([]byte, 8)
	_, _ = rand.Read(randomBytes)
	for i := 0; i < 8; i++ {
		b[i] = chars[int(randomBytes[i])%len(chars)]
	}
	return string(b)
}

func CreateProject(database *db.DB, userID int64, name, description, color string) (*Project, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 64 {
		return nil, ErrInvalidProjectName
	}
	description = strings.TrimSpace(description)
	if color == "" {
		color = PaletteColors[0]
	}

	inviteCode := GenerateInviteCode()
	now := time.Now().UTC().Format(time.RFC3339)

	tx, err := database.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to start tx: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.Exec(`
		INSERT INTO projects (name, description, invite_code, color, created_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, name, description, inviteCode, color, userID, now, now)
	if err != nil {
		return nil, fmt.Errorf("failed to insert project: %w", err)
	}

	pid, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}

	_, err = tx.Exec(`
		INSERT INTO project_members (project_id, user_id, role, color, joined_at)
		VALUES (?, ?, 'owner', ?, ?)
	`, pid, userID, color, now)
	if err != nil {
		return nil, fmt.Errorf("failed to insert project owner: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit tx: %w", err)
	}

	return &Project{
		ID:          pid,
		Name:        name,
		Description: description,
		InviteCode:  inviteCode,
		Color:       color,
		CreatedBy:   userID,
		CreatedAt:   now,
		UpdatedAt:   now,
		MemberCount: 1,
		Role:        "owner",
	}, nil
}

func JoinProjectByInviteCode(database *db.DB, userID int64, inviteCode string) (*Project, error) {
	inviteCode = strings.ToUpper(strings.TrimSpace(inviteCode))
	if inviteCode == "" {
		return nil, ErrInvalidInviteCode
	}

	var p Project
	err := database.QueryRow(`
		SELECT id, name, description, invite_code, color, created_by, created_at, updated_at
		FROM projects
		WHERE invite_code = ?
	`, inviteCode).Scan(&p.ID, &p.Name, &p.Description, &p.InviteCode, &p.Color, &p.CreatedBy, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrInvalidInviteCode
		}
		return nil, fmt.Errorf("query project by invite code failed: %w", err)
	}

	// 检查是否已是成员
	var count int
	err = database.QueryRow(`
		SELECT COUNT(*) FROM project_members WHERE project_id = ? AND user_id = ?
	`, p.ID, userID).Scan(&count)
	if err == nil && count > 0 {
		return &p, nil
	}

	// 选取该项目中尚未被占用的推荐颜色
	memberColor := PickUnusedColor(database, p.ID)
	now := time.Now().UTC().Format(time.RFC3339)

	_, err = database.Exec(`
		INSERT INTO project_members (project_id, user_id, role, color, joined_at)
		VALUES (?, ?, 'member', ?, ?)
	`, p.ID, userID, memberColor, now)
	if err != nil {
		return nil, fmt.Errorf("failed to join project: %w", err)
	}

	p.Role = "member"
	return &p, nil
}

func PickUnusedColor(database *db.DB, projectID int64) string {
	rows, err := database.Query(`SELECT color FROM project_members WHERE project_id = ?`, projectID)
	if err != nil {
		return PaletteColors[0]
	}
	defer rows.Close()

	usedColors := make(map[string]bool)
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err == nil {
			usedColors[c] = true
		}
	}

	for _, c := range PaletteColors {
		if !usedColors[c] {
			return c
		}
	}
	return PaletteColors[0]
}

func GetProjectDetails(database *db.DB, projectID int64, userID int64) (*Project, *Member, error) {
	row := database.QueryRow(`
		SELECT p.id, p.name, p.description, p.invite_code, p.color, p.created_by, p.created_at, p.updated_at,
		       pm.role, pm.color, pm.joined_at,
		       (SELECT COUNT(*) FROM project_members WHERE project_id = p.id) as member_count
		FROM projects p
		JOIN project_members pm ON p.id = pm.project_id
		WHERE p.id = ? AND pm.user_id = ?
	`, projectID, userID)

	var p Project
	var m Member
	m.ProjectID = projectID
	m.UserID = userID

	err := row.Scan(
		&p.ID, &p.Name, &p.Description, &p.InviteCode, &p.Color, &p.CreatedBy, &p.CreatedAt, &p.UpdatedAt,
		&m.Role, &m.Color, &m.JoinedAt,
		&p.MemberCount,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, ErrNotMember
		}
		return nil, nil, fmt.Errorf("query project details failed: %w", err)
	}
	p.Role = m.Role

	return &p, &m, nil
}

func ListProjectMembers(database *db.DB, projectID int64) ([]Member, error) {
	rows, err := database.Query(`
		SELECT pm.project_id, pm.user_id, u.username, u.display_name, u.avatar_key, pm.role, pm.color, pm.joined_at
		FROM project_members pm
		JOIN users u ON pm.user_id = u.id
		WHERE pm.project_id = ?
		ORDER BY CASE WHEN pm.role = 'owner' THEN 0 ELSE 1 END, pm.joined_at ASC
	`, projectID)
	if err != nil {
		return nil, fmt.Errorf("query project members failed: %w", err)
	}
	defer rows.Close()

	var members []Member
	for rows.Next() {
		var m Member
		var avatar sql.NullString
		if err := rows.Scan(&m.ProjectID, &m.UserID, &m.Username, &m.DisplayName, &avatar, &m.Role, &m.Color, &m.JoinedAt); err != nil {
			return nil, err
		}
		if avatar.Valid {
			m.AvatarKey = &avatar.String
		}
		members = append(members, m)
	}
	return members, nil
}

func UpdateMemberColor(database *db.DB, projectID int64, userID int64, color string) error {
	color = strings.TrimSpace(color)
	if color == "" {
		return errors.New("颜色不能为空")
	}
	_, err := database.Exec(`
		UPDATE project_members
		SET color = ?
		WHERE project_id = ? AND user_id = ?
	`, color, projectID, userID)
	if err != nil {
		return fmt.Errorf("failed to update member color: %w", err)
	}
	return nil
}

func RemoveMember(database *db.DB, projectID int64, operatorID int64, targetUserID int64) error {
	var operatorRole string
	err := database.QueryRow(`
		SELECT role FROM project_members WHERE project_id = ? AND user_id = ?
	`, projectID, operatorID).Scan(&operatorRole)
	if err != nil {
		return ErrNotMember
	}

	var targetRole string
	err = database.QueryRow(`
		SELECT role FROM project_members WHERE project_id = ? AND user_id = ?
	`, projectID, targetUserID).Scan(&targetRole)
	if err != nil {
		return ErrNotMember
	}

	// 退出自己
	if operatorID == targetUserID {
		if operatorRole == "owner" {
			return ErrOwnerCannotLeave
		}
		_, err := database.Exec(`
			DELETE FROM project_members WHERE project_id = ? AND user_id = ?
		`, projectID, targetUserID)
		return err
	}

	// 移除别人，必须是 owner 且不能移除 owner
	if operatorRole != "owner" {
		return ErrNotOwner
	}
	if targetRole == "owner" {
		return errors.New("不能移除项目所有者")
	}

	_, err = database.Exec(`
		DELETE FROM project_members WHERE project_id = ? AND user_id = ?
	`, projectID, targetUserID)
	return err
}

func ResetInviteCode(database *db.DB, projectID int64, operatorID int64) (string, error) {
	var role string
	err := database.QueryRow(`
		SELECT role FROM project_members WHERE project_id = ? AND user_id = ?
	`, projectID, operatorID).Scan(&role)
	if err != nil || role != "owner" {
		return "", ErrNotOwner
	}

	newCode := GenerateInviteCode()
	now := time.Now().UTC().Format(time.RFC3339)

	_, err = database.Exec(`
		UPDATE projects
		SET invite_code = ?, updated_at = ?
		WHERE id = ?
	`, newCode, now, projectID)
	if err != nil {
		return "", fmt.Errorf("failed to reset invite code: %w", err)
	}

	return newCode, nil
}

func UpdateProject(database *db.DB, projectID int64, operatorID int64, name, description, color string) error {
	var role string
	err := database.QueryRow(`
		SELECT role FROM project_members WHERE project_id = ? AND user_id = ?
	`, projectID, operatorID).Scan(&role)
	if err != nil || role != "owner" {
		return ErrNotOwner
	}

	name = strings.TrimSpace(name)
	if name == "" || len(name) > 64 {
		return ErrInvalidProjectName
	}
	now := time.Now().UTC().Format(time.RFC3339)

	_, err = database.Exec(`
		UPDATE projects
		SET name = ?, description = ?, color = ?, updated_at = ?
		WHERE id = ?
	`, name, strings.TrimSpace(description), color, now, projectID)
	if err != nil {
		return fmt.Errorf("failed to update project: %w", err)
	}

	return nil
}

func DeleteProject(database *db.DB, projectID int64, operatorID int64) error {
	var role string
	err := database.QueryRow(`
		SELECT role FROM project_members WHERE project_id = ? AND user_id = ?
	`, projectID, operatorID).Scan(&role)
	if err != nil || role != "owner" {
		return ErrNotOwner
	}

	_, err = database.Exec(`DELETE FROM projects WHERE id = ?`, projectID)
	if err != nil {
		return fmt.Errorf("failed to delete project: %w", err)
	}
	return nil
}

func GetProjectsForUser(database *db.DB, userID int64) (created []Project, joined []Project, err error) {
	rows, err := database.Query(`
		SELECT p.id, p.name, p.description, p.invite_code, p.color, p.created_by, p.created_at, p.updated_at,
		       pm.role,
		       (SELECT COUNT(*) FROM project_members WHERE project_id = p.id) as member_count
		FROM projects p
		JOIN project_members pm ON p.id = pm.project_id
		WHERE pm.user_id = ?
		ORDER BY p.updated_at DESC
	`, userID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to query user projects: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var p Project
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.InviteCode, &p.Color, &p.CreatedBy, &p.CreatedAt, &p.UpdatedAt, &p.Role, &p.MemberCount); err != nil {
			return nil, nil, err
		}
		if p.CreatedBy == userID {
			created = append(created, p)
		} else {
			joined = append(joined, p)
		}
	}
	return created, joined, nil
}
