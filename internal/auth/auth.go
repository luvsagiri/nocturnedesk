package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/luvsagiri/nocturnedesk/internal/db"
)

var (
	usernameRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]{3,32}$`)

	ErrInvalidUsername     = errors.New("用户名只允许包含英文字母、数字、下划线及减号，长度在 3-32 位之间")
	ErrUsernameTaken       = errors.New("该用户名已被注册")
	ErrEmptyDisplayName    = errors.New("显示名称不能为空")
	ErrInvalidPassword     = errors.New("密码长度需在 1-128 位之间")
	ErrInvalidCredentials  = errors.New("用户名或密码错误")
	ErrUserNotFound        = errors.New("用户不存在")
	ErrIncorrectPassword   = errors.New("原密码错误")
)

type User struct {
	ID           int64   `json:"id"`
	Username     string  `json:"username"`
	DisplayName  string  `json:"display_name"`
	PasswordHash string  `json:"-"`
	AvatarKey    *string `json:"avatar_key"`
	CreatedAt    string  `json:"created_at"`
}

type contextKey string

const UserContextKey contextKey = "nd_current_user"

func UserFromContext(ctx context.Context) *User {
	if u, ok := ctx.Value(UserContextKey).(*User); ok {
		return u
	}
	return nil
}

func ContextWithUser(ctx context.Context, u *User) context.Context {
	return context.WithValue(ctx, UserContextKey, u)
}

func Register(database *db.DB, username, displayName, password string) (*User, error) {
	username = strings.TrimSpace(username)
	if !usernameRegex.MatchString(username) {
		return nil, ErrInvalidUsername
	}
	usernameLower := strings.ToLower(username)

	displayName = strings.TrimSpace(displayName)
	if displayName == "" || len(displayName) > 64 {
		return nil, ErrEmptyDisplayName
	}

	if len(password) < 1 || len(password) > 128 {
		return nil, ErrInvalidPassword
	}

	// 检查用户名是否重复
	var count int
	err := database.QueryRow("SELECT COUNT(*) FROM users WHERE username = ?", usernameLower).Scan(&count)
	if err != nil {
		return nil, fmt.Errorf("检查用户名失败: %w", err)
	}
	if count > 0 {
		return nil, ErrUsernameTaken
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("密码加密失败: %w", err)
	}

	createdAt := time.Now().UTC().Format(time.RFC3339)
	res, err := database.Exec(`
		INSERT INTO users (username, display_name, password_hash, avatar_key, created_at)
		VALUES (?, ?, ?, NULL, ?)
	`, usernameLower, displayName, string(hash), createdAt)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, ErrUsernameTaken
		}
		return nil, fmt.Errorf("插入用户失败: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("获取新用户ID失败: %w", err)
	}

	return &User{
		ID:           id,
		Username:     usernameLower,
		DisplayName:  displayName,
		PasswordHash: string(hash),
		AvatarKey:    nil,
		CreatedAt:    createdAt,
	}, nil
}

func Authenticate(database *db.DB, username, password string) (*User, error) {
	usernameLower := strings.ToLower(strings.TrimSpace(username))
	if usernameLower == "" || password == "" {
		return nil, ErrInvalidCredentials
	}

	row := database.QueryRow(`
		SELECT id, username, display_name, password_hash, avatar_key, created_at
		FROM users
		WHERE username = ?
	`, usernameLower)

	var u User
	var avatar sql.NullString
	if err := row.Scan(&u.ID, &u.Username, &u.DisplayName, &u.PasswordHash, &avatar, &u.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrInvalidCredentials
		}
		return nil, fmt.Errorf("查询用户失败: %w", err)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		return nil, ErrInvalidCredentials
	}

	if avatar.Valid {
		u.AvatarKey = &avatar.String
	}

	return &u, nil
}

func GetUserByID(database *db.DB, id int64) (*User, error) {
	row := database.QueryRow(`
		SELECT id, username, display_name, password_hash, avatar_key, created_at
		FROM users
		WHERE id = ?
	`, id)

	var u User
	var avatar sql.NullString
	if err := row.Scan(&u.ID, &u.Username, &u.DisplayName, &u.PasswordHash, &avatar, &u.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("获取用户失败: %w", err)
	}

	if avatar.Valid {
		u.AvatarKey = &avatar.String
	}

	return &u, nil
}

func UpdateProfile(database *db.DB, userID int64, displayName string, avatarKey *string) error {
	displayName = strings.TrimSpace(displayName)
	if displayName == "" || len(displayName) > 64 {
		return ErrEmptyDisplayName
	}

	var avatarVal sql.NullString
	if avatarKey != nil && *avatarKey != "" {
		avatarVal = sql.NullString{String: *avatarKey, Valid: true}
	}

	_, err := database.Exec(`
		UPDATE users
		SET display_name = ?, avatar_key = ?
		WHERE id = ?
	`, displayName, avatarVal, userID)
	if err != nil {
		return fmt.Errorf("更新资料失败: %w", err)
	}
	return nil
}

func UpdatePassword(database *db.DB, userID int64, oldPassword, newPassword string) error {
	if len(newPassword) < 1 || len(newPassword) > 128 {
		return ErrInvalidPassword
	}

	var hash string
	err := database.QueryRow("SELECT password_hash FROM users WHERE id = ?", userID).Scan(&hash)
	if err != nil {
		return ErrUserNotFound
	}

	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(oldPassword)); err != nil {
		return ErrIncorrectPassword
	}

	newHash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("生成新密码哈希失败: %w", err)
	}

	_, err = database.Exec("UPDATE users SET password_hash = ? WHERE id = ?", string(newHash), userID)
	if err != nil {
		return fmt.Errorf("更新密码失败: %w", err)
	}

	return nil
}
