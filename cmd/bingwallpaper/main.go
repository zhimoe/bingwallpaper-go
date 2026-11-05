package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/zhimoe/bingwallpaper/internal/app"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := app.Run(ctx, os.Args[1:]); err != nil {
		slog.Error("运行失败", "error", err)
		fmt.Fprintln(os.Stderr, "错误：", err)
		os.Exit(1)
	}
}
