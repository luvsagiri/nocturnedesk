package user

import (
	"database/sql"
	"fmt"

	"github.com/luvsagiri/nocturnedesk/internal/db"
)

type MyCardItem struct {
	ID          int64   `json:"id"`
	ProjectID   int64   `json:"project_id"`
	ProjectName string  `json:"project_name"`
	Title       string  `json:"title"`
	Status      string  `json:"status"`
	DueDate     *string `json:"due_date"`
}

type MyCalendarItem struct {
	ID          int64  `json:"id"`
	ProjectID   int64  `json:"project_id"`
	ProjectName string `json:"project_name"`
	Day         string `json:"day"`
	Content     string `json:"content"`
}

type MyDocItem struct {
	ID          int64  `json:"id"`
	ProjectID   int64  `json:"project_id"`
	ProjectName string `json:"project_name"`
	Title       string `json:"title"`
	UpdatedAt   string `json:"updated_at"`
}

func GetMyCards(database *db.DB, userID int64) ([]MyCardItem, error) {
	rows, err := database.Query(`
		SELECT c.id, c.project_id, p.name, c.title, c.status, c.due_date
		FROM cards c
		JOIN projects p ON c.project_id = p.id
		JOIN project_members pm ON p.id = pm.project_id
		WHERE c.assignee_id = ? AND pm.user_id = ?
		ORDER BY c.updated_at DESC
	`, userID, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to query my cards: %w", err)
	}
	defer rows.Close()

	var cards []MyCardItem
	for rows.Next() {
		var item MyCardItem
		var dueDate sql.NullString
		if err := rows.Scan(&item.ID, &item.ProjectID, &item.ProjectName, &item.Title, &item.Status, &dueDate); err != nil {
			return nil, err
		}
		if dueDate.Valid {
			item.DueDate = &dueDate.String
		}
		cards = append(cards, item)
	}
	return cards, nil
}

func GetMyCalendarEntries(database *db.DB, userID int64) ([]MyCalendarItem, error) {
	rows, err := database.Query(`
		SELECT ce.id, ce.project_id, p.name, ce.day, ce.content
		FROM calendar_entries ce
		JOIN projects p ON ce.project_id = p.id
		JOIN project_members pm ON p.id = pm.project_id
		WHERE ce.user_id = ? AND pm.user_id = ?
		ORDER BY ce.day DESC
	`, userID, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to query my calendar: %w", err)
	}
	defer rows.Close()

	var entries []MyCalendarItem
	for rows.Next() {
		var item MyCalendarItem
		if err := rows.Scan(&item.ID, &item.ProjectID, &item.ProjectName, &item.Day, &item.Content); err != nil {
			return nil, err
		}
		entries = append(entries, item)
	}
	return entries, nil
}

func GetMyDocs(database *db.DB, userID int64) ([]MyDocItem, error) {
	rows, err := database.Query(`
		SELECT i.id, i.project_id, p.name, i.title, i.updated_at
		FROM items i
		JOIN projects p ON i.project_id = p.id
		JOIN project_members pm ON p.id = pm.project_id
		WHERE i.created_by = ? AND i.kind = 'doc' AND pm.user_id = ?
		ORDER BY i.updated_at DESC
	`, userID, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to query my docs: %w", err)
	}
	defer rows.Close()

	var docs []MyDocItem
	for rows.Next() {
		var item MyDocItem
		if err := rows.Scan(&item.ID, &item.ProjectID, &item.ProjectName, &item.Title, &item.UpdatedAt); err != nil {
			return nil, err
		}
		docs = append(docs, item)
	}
	return docs, nil
}
