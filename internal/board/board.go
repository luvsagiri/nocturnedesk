package board

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/luvsagiri/nocturnedesk/internal/db"
	"github.com/luvsagiri/nocturnedesk/internal/project"
)

var (
	ErrCardNotFound       = errors.New("卡片不存在")
	ErrLabelNotFound      = errors.New("标签不存在")
	ErrInvalidCardTitle   = errors.New("卡片标题不能为空且不超过 128 个字符")
	ErrInvalidLabelName   = errors.New("标签名称不能为空且不超过 32 个字符")
	ErrInvalidCardStatus  = errors.New("无效的卡片状态")
	ErrLabelAlreadyExists = errors.New("该标签名称已存在")
)

type Label struct {
	ID        int64  `json:"id"`
	ProjectID int64  `json:"project_id"`
	Name      string `json:"name"`
	Color     string `json:"color"`
}

type Card struct {
	ID            int64   `json:"id"`
	ProjectID     int64   `json:"project_id"`
	Title         string  `json:"title"`
	Description   string  `json:"description"`
	AssigneeID    *int64  `json:"assignee_id"`
	AssigneeName  *string `json:"assignee_name"`
	AssigneeColor *string `json:"assignee_color"`
	Status        string  `json:"status"` // 'todo', 'doing', 'done'
	Position      float64 `json:"position"`
	DueDate       *string `json:"due_date"`
	CreatedBy     int64   `json:"created_by"`
	CreatedAt     string  `json:"created_at"`
	UpdatedAt     string  `json:"updated_at"`
	Labels        []Label `json:"labels"`
}

type BoardView struct {
	TodoCards  []Card           `json:"todo_cards"`
	DoingCards []Card           `json:"doing_cards"`
	DoneCards  []Card           `json:"done_cards"`
	Labels     []Label          `json:"labels"`
	Members    []project.Member `json:"members"`
}

func GetBoardView(database *db.DB, projectID int64) (*BoardView, error) {
	labels, err := ListLabels(database, projectID)
	if err != nil {
		return nil, err
	}

	members, err := project.ListProjectMembers(database, projectID)
	if err != nil {
		return nil, err
	}

	// 查询所有卡片
	rows, err := database.Query(`
		SELECT c.id, c.project_id, c.title, c.description, c.assignee_id,
		       u.display_name, pm.color,
		       c.status, c.position, c.due_date, c.created_by, c.created_at, c.updated_at
		FROM cards c
		LEFT JOIN users u ON c.assignee_id = u.id
		LEFT JOIN project_members pm ON (c.project_id = pm.project_id AND c.assignee_id = pm.user_id)
		WHERE c.project_id = ?
		ORDER BY c.position ASC, c.id ASC
	`, projectID)
	if err != nil {
		return nil, fmt.Errorf("failed to query cards: %w", err)
	}
	defer rows.Close()

	var allCards []Card
	cardMap := make(map[int64]*Card)

	for rows.Next() {
		var c Card
		var aID sql.NullInt64
		var aName, aColor, dueDate sql.NullString
		if err := rows.Scan(
			&c.ID, &c.ProjectID, &c.Title, &c.Description, &aID,
			&aName, &aColor,
			&c.Status, &c.Position, &dueDate, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt,
		); err != nil {
			return nil, err
		}

		if aID.Valid {
			c.AssigneeID = &aID.Int64
		}
		if aName.Valid {
			c.AssigneeName = &aName.String
		}
		if aColor.Valid {
			c.AssigneeColor = &aColor.String
		}
		if dueDate.Valid {
			c.DueDate = &dueDate.String
		}
		c.Labels = []Label{}
		allCards = append(allCards, c)
	}

	// 关联标签
	labelRows, err := database.Query(`
		SELECT cl.card_id, l.id, l.project_id, l.name, l.color
		FROM card_labels cl
		JOIN labels l ON cl.label_id = l.id
		WHERE l.project_id = ?
	`, projectID)
	if err == nil {
		defer labelRows.Close()
		cardLabelsMap := make(map[int64][]Label)
		for labelRows.Next() {
			var cardID int64
			var l Label
			if err := labelRows.Scan(&cardID, &l.ID, &l.ProjectID, &l.Name, &l.Color); err == nil {
				cardLabelsMap[cardID] = append(cardLabelsMap[cardID], l)
			}
		}
		for i := range allCards {
			if lbls, ok := cardLabelsMap[allCards[i].ID]; ok {
				allCards[i].Labels = lbls
			}
			cardMap[allCards[i].ID] = &allCards[i]
		}
	}

	view := &BoardView{
		Labels:  labels,
		Members: members,
	}

	for _, c := range allCards {
		switch c.Status {
		case "todo":
			view.TodoCards = append(view.TodoCards, c)
		case "doing":
			view.DoingCards = append(view.DoingCards, c)
		case "done":
			view.DoneCards = append(view.DoneCards, c)
		}
	}

	return view, nil
}

func CreateCard(database *db.DB, projectID, userID int64, title, description string, assigneeID *int64, dueDate *string, labelIDs []int64) (*Card, error) {
	title = strings.TrimSpace(title)
	if title == "" || len(title) > 128 {
		return nil, ErrInvalidCardTitle
	}

	// 获取 todo 列的最大 position
	var maxPos sql.NullFloat64
	_ = database.QueryRow(`
		SELECT MAX(position) FROM cards WHERE project_id = ? AND status = 'todo'
	`, projectID).Scan(&maxPos)

	nextPos := 1000.0
	if maxPos.Valid {
		nextPos = maxPos.Float64 + 1000.0
	}

	var aIDVal sql.NullInt64
	if assigneeID != nil && *assigneeID > 0 {
		aIDVal = sql.NullInt64{Int64: *assigneeID, Valid: true}
	}

	var dDateVal sql.NullString
	if dueDate != nil && strings.TrimSpace(*dueDate) != "" {
		dDateVal = sql.NullString{String: strings.TrimSpace(*dueDate), Valid: true}
	}

	now := time.Now().UTC().Format(time.RFC3339)

	tx, err := database.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	res, err := tx.Exec(`
		INSERT INTO cards (project_id, title, description, assignee_id, status, position, due_date, created_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, 'todo', ?, ?, ?, ?, ?)
	`, projectID, title, strings.TrimSpace(description), aIDVal, nextPos, dDateVal, userID, now, now)
	if err != nil {
		return nil, fmt.Errorf("insert card failed: %w", err)
	}

	cardID, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}

	// 插入标签关联
	for _, lid := range labelIDs {
		if lid > 0 {
			_, _ = tx.Exec(`INSERT OR IGNORE INTO card_labels (card_id, label_id) VALUES (?, ?)`, cardID, lid)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	// 同步 search_index
	_, _ = database.Exec(`
		INSERT INTO search_index (title, body, project_id, kind, ref_id)
		VALUES (?, ?, ?, 'card', ?)
	`, title, description, projectID, cardID)

	return GetCard(database, cardID)
}

func GetCard(database *db.DB, cardID int64) (*Card, error) {
	row := database.QueryRow(`
		SELECT c.id, c.project_id, c.title, c.description, c.assignee_id,
		       u.display_name, pm.color,
		       c.status, c.position, c.due_date, c.created_by, c.created_at, c.updated_at
		FROM cards c
		LEFT JOIN users u ON c.assignee_id = u.id
		LEFT JOIN project_members pm ON (c.project_id = pm.project_id AND c.assignee_id = pm.user_id)
		WHERE c.id = ?
	`, cardID)

	var c Card
	var aID sql.NullInt64
	var aName, aColor, dueDate sql.NullString
	if err := row.Scan(
		&c.ID, &c.ProjectID, &c.Title, &c.Description, &aID,
		&aName, &aColor,
		&c.Status, &c.Position, &dueDate, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrCardNotFound
		}
		return nil, err
	}

	if aID.Valid {
		c.AssigneeID = &aID.Int64
	}
	if aName.Valid {
		c.AssigneeName = &aName.String
	}
	if aColor.Valid {
		c.AssigneeColor = &aColor.String
	}
	if dueDate.Valid {
		c.DueDate = &dueDate.String
	}

	// 填充标签
	rows, err := database.Query(`
		SELECT l.id, l.project_id, l.name, l.color
		FROM card_labels cl
		JOIN labels l ON cl.label_id = l.id
		WHERE cl.card_id = ?
	`, cardID)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var l Label
			if err := rows.Scan(&l.ID, &l.ProjectID, &l.Name, &l.Color); err == nil {
				c.Labels = append(c.Labels, l)
			}
		}
	}

	return &c, nil
}

func UpdateCard(database *db.DB, cardID int64, title, description string, assigneeID *int64, dueDate *string, labelIDs []int64) error {
	title = strings.TrimSpace(title)
	if title == "" || len(title) > 128 {
		return ErrInvalidCardTitle
	}

	var aIDVal sql.NullInt64
	if assigneeID != nil && *assigneeID > 0 {
		aIDVal = sql.NullInt64{Int64: *assigneeID, Valid: true}
	}

	var dDateVal sql.NullString
	if dueDate != nil && strings.TrimSpace(*dueDate) != "" {
		dDateVal = sql.NullString{String: strings.TrimSpace(*dueDate), Valid: true}
	}

	now := time.Now().UTC().Format(time.RFC3339)

	tx, err := database.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec(`
		UPDATE cards
		SET title = ?, description = ?, assignee_id = ?, due_date = ?, updated_at = ?
		WHERE id = ?
	`, title, strings.TrimSpace(description), aIDVal, dDateVal, now, cardID)
	if err != nil {
		return fmt.Errorf("update card failed: %w", err)
	}

	// 更新标签关联
	_, _ = tx.Exec("DELETE FROM card_labels WHERE card_id = ?", cardID)
	for _, lid := range labelIDs {
		if lid > 0 {
			_, _ = tx.Exec("INSERT OR IGNORE INTO card_labels (card_id, label_id) VALUES (?, ?)", cardID, lid)
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	// 同步 search_index
	var pid int64
	_ = database.QueryRow("SELECT project_id FROM cards WHERE id = ?", cardID).Scan(&pid)
	_, _ = database.Exec("DELETE FROM search_index WHERE kind = 'card' AND ref_id = ?", cardID)
	_, _ = database.Exec(`
		INSERT INTO search_index (title, body, project_id, kind, ref_id)
		VALUES (?, ?, ?, 'card', ?)
	`, title, description, pid, cardID)

	return nil
}

func MoveCard(database *db.DB, cardID int64, newStatus string, prevCardID, nextCardID *int64) error {
	if newStatus != "todo" && newStatus != "doing" && newStatus != "done" {
		return ErrInvalidCardStatus
	}

	c, err := GetCard(database, cardID)
	if err != nil {
		return err
	}

	var newPos float64

	if prevCardID == nil && nextCardID == nil {
		// 目标列为空，直接放 1000
		newPos = 1000.0
	} else if prevCardID == nil && nextCardID != nil {
		// 放在列首
		nextCard, err := GetCard(database, *nextCardID)
		if err != nil {
			newPos = 1000.0
		} else {
			newPos = nextCard.Position / 2.0
			if newPos <= 0.0001 {
				// 精度不足，触发整列重排
				defer reindexColumn(database, c.ProjectID, newStatus)
				newPos = 500.0
			}
		}
	} else if prevCardID != nil && nextCardID == nil {
		// 放在列尾
		prevCard, err := GetCard(database, *prevCardID)
		if err != nil {
			newPos = 1000.0
		} else {
			newPos = prevCard.Position + 1000.0
		}
	} else {
		// 插入在 prevCard 与 nextCard 之间
		prevCard, err1 := GetCard(database, *prevCardID)
		nextCard, err2 := GetCard(database, *nextCardID)
		if err1 == nil && err2 == nil {
			diff := nextCard.Position - prevCard.Position
			if math.Abs(diff) < 1e-4 {
				// 触发整列重排并取标准中点
				newPos = prevCard.Position + 500.0
				defer reindexColumn(database, c.ProjectID, newStatus)
			} else {
				newPos = (prevCard.Position + nextCard.Position) / 2.0
			}
		} else {
			newPos = 1000.0
		}
	}

	now := time.Now().UTC().Format(time.RFC3339)
	_, err = database.Exec(`
		UPDATE cards
		SET status = ?, position = ?, updated_at = ?
		WHERE id = ?
	`, newStatus, newPos, now, cardID)
	return err
}

func reindexColumn(database *db.DB, projectID int64, status string) {
	rows, err := database.Query(`
		SELECT id FROM cards
		WHERE project_id = ? AND status = ?
		ORDER BY position ASC, id ASC
	`, projectID, status)
	if err != nil {
		return
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err == nil {
			ids = append(ids, id)
		}
	}

	tx, err := database.Begin()
	if err != nil {
		return
	}
	defer tx.Rollback()

	pos := 1000.0
	for _, id := range ids {
		_, _ = tx.Exec("UPDATE cards SET position = ? WHERE id = ?", pos, id)
		pos += 1000.0
	}
	_ = tx.Commit()
}

func DeleteCard(database *db.DB, cardID int64) error {
	_, err := database.Exec("DELETE FROM cards WHERE id = ?", cardID)
	if err != nil {
		return err
	}
	_, _ = database.Exec("DELETE FROM search_index WHERE kind = 'card' AND ref_id = ?", cardID)
	return nil
}

func ListLabels(database *db.DB, projectID int64) ([]Label, error) {
	rows, err := database.Query(`
		SELECT id, project_id, name, color
		FROM labels
		WHERE project_id = ?
		ORDER BY name ASC
	`, projectID)
	if err != nil {
		return nil, fmt.Errorf("failed to query labels: %w", err)
	}
	defer rows.Close()

	var labels []Label
	for rows.Next() {
		var l Label
		if err := rows.Scan(&l.ID, &l.ProjectID, &l.Name, &l.Color); err != nil {
			return nil, err
		}
		labels = append(labels, l)
	}
	return labels, nil
}

func CreateLabel(database *db.DB, projectID int64, name, color string) (*Label, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 32 {
		return nil, ErrInvalidLabelName
	}
	color = strings.TrimSpace(color)
	if color == "" {
		color = "#6366f1"
	}

	res, err := database.Exec(`
		INSERT INTO labels (project_id, name, color)
		VALUES (?, ?, ?)
	`, projectID, name, color)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, ErrLabelAlreadyExists
		}
		return nil, fmt.Errorf("failed to insert label: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}

	return &Label{
		ID:        id,
		ProjectID: projectID,
		Name:      name,
		Color:     color,
	}, nil
}

func UpdateLabel(database *db.DB, labelID int64, name, color string) error {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 32 {
		return ErrInvalidLabelName
	}
	color = strings.TrimSpace(color)
	if color == "" {
		color = "#6366f1"
	}

	_, err := database.Exec(`
		UPDATE labels
		SET name = ?, color = ?
		WHERE id = ?
	`, name, color, labelID)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return ErrLabelAlreadyExists
		}
		return fmt.Errorf("failed to update label: %w", err)
	}
	return nil
}

func DeleteLabel(database *db.DB, labelID int64) error {
	// 外键 ON DELETE CASCADE 会自动清理 card_labels
	_, err := database.Exec("DELETE FROM labels WHERE id = ?", labelID)
	return err
}
