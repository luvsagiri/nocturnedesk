package web_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/luvsagiri/nocturnedesk/internal/config"
	"github.com/luvsagiri/nocturnedesk/internal/db"
	"github.com/luvsagiri/nocturnedesk/internal/web"
)

func TestSlice1HealthzAndDB(t *testing.T) {
	tempDir := t.TempDir()
	cfg := &config.Config{
		Port:            "8080",
		DataDir:         tempDir,
		MaxUploadMB:     200,
		SessionDuration: 30 * 24 * 3600 * 1e9,
		SecretKey:       []byte("01234567890123456789012345678901"),
	}

	database, err := db.Connect(cfg.DBPath())
	if err != nil {
		t.Fatalf("Failed to connect and migrate db: %v", err)
	}
	defer database.Close()

	// Verify tables exist
	var count int
	err = database.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&count)
	if err != nil {
		t.Fatalf("schema_migrations table does not exist: %v", err)
	}
	if count == 0 {
		t.Fatalf("no migrations applied")
	}

	// Verify users table exists
	err = database.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	if err != nil {
		t.Fatalf("users table does not exist: %v", err)
	}

	// Test server and /healthz
	srv := web.NewServer(cfg, database, os.DirFS(filepath.Join("..", "..")), os.DirFS(filepath.Join("..", "..")))
	req := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()

	srv.ServeHTTP(w, req)
	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	body := w.Body.String()
	if body != "ok" {
		t.Fatalf("expected 'ok', got '%s'", body)
	}
}
