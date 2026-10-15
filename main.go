package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/luvsagiri/nocturnedesk/internal/config"
	"github.com/luvsagiri/nocturnedesk/internal/db"
	"github.com/luvsagiri/nocturnedesk/internal/web"
)

func main() {
	// 检查是否有 CLI 子命令：user passwd <username>
	if len(os.Args) >= 4 && os.Args[1] == "user" && os.Args[2] == "passwd" {
		handleCLIUserPasswd(os.Args[3])
		return
	}

	cfg, err := config.Load(os.Args[1:])
	if err != nil {
		log.Fatalf("配置加载失败: %v", err)
	}

	database, err := db.Connect(cfg.DBPath())
	if err != nil {
		log.Fatalf("数据库连接或迁移失败: %v", err)
	}
	defer database.Close()

	srv := web.NewServer(cfg, database, templatesFS, staticFS)

	addr := ":" + cfg.Port
	httpServer := &http.Server{
		Addr:    addr,
		Handler: srv.Handler(),
	}

	// 监听系统信号：SIGINT (Ctrl+C), SIGTERM (kill / docker stop)
	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		log.Printf("NocturneDesk 启动成功: http://127.0.0.1%s (数据目录: %s)", addr, cfg.DataDir)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("服务器异常退出: %v", err)
		}
	}()

	sig := <-stopChan
	log.Printf("收到终止信号 (%v)，正在安全退出...", sig)

	// 优雅停机超时时间设为 5 秒
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("强制关闭 HTTP 服务: %v", err)
	} else {
		log.Println("NocturneDesk 已平滑安全退出")
	}
}

func handleCLIUserPasswd(username string) {
	username = strings.ToLower(strings.TrimSpace(username))
	if username == "" {
		fmt.Println("错误: 用户名不能为空")
		os.Exit(1)
	}

	cfg, err := config.Load(nil)
	if err != nil {
		fmt.Printf("加载配置失败: %v\n", err)
		os.Exit(1)
	}

	database, err := db.Connect(cfg.DBPath())
	if err != nil {
		fmt.Printf("数据库连接失败: %v\n", err)
		os.Exit(1)
	}
	defer database.Close()

	var count int
	err = database.QueryRow("SELECT COUNT(*) FROM users WHERE username = ?", username).Scan(&count)
	if err != nil || count == 0 {
		fmt.Printf("错误: 用户 '%s' 不存在\n", username)
		os.Exit(1)
	}

	fmt.Printf("请输入用户 '%s' 的新密码: ", username)
	reader := bufio.NewReader(os.Stdin)
	newPass, _ := reader.ReadString('\n')
	newPass = strings.TrimRight(newPass, "\r\n")
	if newPass == "" {
		fmt.Println("错误: 密码不能为空")
		os.Exit(1)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(newPass), bcrypt.DefaultCost)
	if err != nil {
		fmt.Printf("密码哈希生成失败: %v\n", err)
		os.Exit(1)
	}

	_, err = database.Exec("UPDATE users SET password_hash = ? WHERE username = ?", string(hash), username)
	if err != nil {
		fmt.Printf("更新密码失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("成功: 用户 '%s' 的密码已更新\n", username)
}
