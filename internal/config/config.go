package config

import (
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Port            string
	DataDir         string
	SecretKey       []byte
	MaxUploadMB     int64
	SessionDuration time.Duration
}

func (c *Config) UploadsDir() string {
	return filepath.Join(c.DataDir, "uploads")
}

func (c *Config) DBPath() string {
	return filepath.Join(c.DataDir, "nocturnedesk.db")
}

func (c *Config) MaxUploadBytes() int64 {
	return c.MaxUploadMB * 1024 * 1024
}

func Load(args []string) (*Config, error) {
	fs := flag.NewFlagSet("nocturnedesk", flag.ContinueOnError)

	defaultPort := getEnv("PORT", "8080")
	defaultData := getEnv("DATA_DIR", "./data")
	defaultMaxMBStr := getEnv("MAX_UPLOAD_MB", "200")
	defaultMaxMB, _ := strconv.ParseInt(defaultMaxMBStr, 10, 64)
	if defaultMaxMB <= 0 {
		defaultMaxMB = 200
	}

	portFlag := fs.String("port", defaultPort, "HTTP server port")
	dataFlag := fs.String("data", defaultData, "Path to data directory")
	maxMBFlag := fs.Int64("max-upload-mb", defaultMaxMB, "Max upload size in MB")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	cfg := &Config{
		Port:            *portFlag,
		DataDir:         *dataFlag,
		MaxUploadMB:     *maxMBFlag,
		SessionDuration: 30 * 24 * time.Hour,
	}

	if err := os.MkdirAll(cfg.DataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data dir %s: %w", cfg.DataDir, err)
	}
	if err := os.MkdirAll(cfg.UploadsDir(), 0755); err != nil {
		return nil, fmt.Errorf("failed to create uploads dir %s: %w", cfg.UploadsDir(), err)
	}

	key, err := initOrLoadSecretKey(cfg.DataDir)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize secret key: %w", err)
	}
	cfg.SecretKey = key

	return cfg, nil
}

func initOrLoadSecretKey(dataDir string) ([]byte, error) {
	if envKey := strings.TrimSpace(os.Getenv("SECRET_KEY")); envKey != "" {
		if decoded, err := hex.DecodeString(envKey); err == nil && len(decoded) >= 32 {
			return decoded, nil
		}
		return []byte(envKey), nil
	}

	keyPath := filepath.Join(dataDir, "secret.key")
	if data, err := os.ReadFile(keyPath); err == nil && len(data) >= 32 {
		return data, nil
	}

	newKey := make([]byte, 32)
	if _, err := rand.Read(newKey); err != nil {
		return nil, err
	}

	if err := os.WriteFile(keyPath, newKey, 0600); err != nil {
		return nil, err
	}
	return newKey, nil
}

func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
