// Command lucky-draw 启动抽奖 HTTP 服务。
//
// 用法：
//
//	go run .                       # 默认监听 :8080
//	PORT=9000 DRAW_COUNT=9 go run . # 环境变量覆盖配置
//
// 所有配置项见 README；配置非法时进程会直接启动失败并说明原因，
// 而不是带着错误参数对外服务。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/RazorEdge-wyh/lucky-draw/internal/config"
	"github.com/RazorEdge-wyh/lucky-draw/internal/httpapi"
)

// version 由构建时的 -ldflags 注入，用于线上确认部署版本。
var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "打印版本号后退出")
	healthOnly := flag.Bool("check", false, "只做配置自检，不启动服务")
	healthcheck := flag.Bool("healthcheck", false, "作为容器健康检查：请求本机 /healthz，成功退出 0，失败退出 1")
	flag.Parse()

	if *showVersion {
		fmt.Printf("lucky-draw %s\n", version)
		return
	}

	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.SetPrefix("[lucky-draw] ")

	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		log.Fatalf("配置非法: %v", err)
	}

	if *healthcheck {
		// distroless 镜像里没有 curl / wget，因此健康检查由二进制自己完成。
		if err := probeSelf(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "healthcheck failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("ok")
		return
	}

	log.Printf("配置生效: %s", describe(cfg))

	if *healthOnly {
		log.Printf("配置自检通过")
		return
	}

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           httpapi.New(cfg).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// 在后台监听，主协程等待退出信号，从而支持优雅关闭。
	errCh := make(chan error, 1)
	go func() {
		log.Printf("lottery service listening on %s (version=%s)", srv.Addr, version)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-errCh:
		log.Fatalf("服务异常退出: %v", err)
	case sig := <-stop:
		log.Printf("收到信号 %s，开始优雅关闭（最多 10s）", sig)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("优雅关闭超时，强制退出: %v", err)
		_ = srv.Close()
	}
	log.Printf("已退出")
}

// describe 把关键配置拼成一行，方便在日志里快速核对线上参数。
func describe(cfg config.Config) string {
	return fmt.Sprintf(
		"addr=:%s drawCount=%d special=%d specialProb=%.4f range=[%d,%d] constrained=%t maxAttempts=%d",
		cfg.Port, cfg.DrawCount, cfg.SpecialNumber, cfg.SpecialProb,
		cfg.MinNumber, cfg.MaxNumber, cfg.Constrained, cfg.MaxAttempts)
}

// probeSelf 请求本机 /healthz，用作容器健康检查。
//
// 之所以让二进制自己做而不是在镜像里塞 curl：最终镜像是 distroless，
// 没有 shell 也没有任何 HTTP 客户端，多装一个工具就多一份攻击面和体积。
func probeSelf(cfg config.Config) error {
	// 监听地址是 ":8080" 这种形式，探测时要连回本机。
	url := fmt.Sprintf("http://127.0.0.1:%s/healthz", cfg.Port)
	client := &http.Client{Timeout: 3 * time.Second}

	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("请求 %s 失败: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s 返回状态码 %d", url, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64))
	if err != nil {
		return fmt.Errorf("读取响应失败: %w", err)
	}
	if strings.TrimSpace(string(body)) != "ok" {
		return fmt.Errorf("响应内容异常: %q", body)
	}
	return nil
}
