package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"cpa-usage-keeper/internal/app"
	"cpa-usage-keeper/internal/logging"
	"cpa-usage-keeper/internal/version"
	"github.com/sirupsen/logrus"
)

func main() {
	logging.ConfigureBootstrap()

	envFile := flag.String("env", "", "path to env file")
	appHost := flag.String("host", "", "listen host (overrides APP_HOST)")
	var showVersion bool
	flag.BoolVar(&showVersion, "v", false, "print version and exit")
	flag.BoolVar(&showVersion, "version", false, "print version and exit")
	flag.Parse()
	if showVersion {
		fmt.Println(version.Version)
		return
	}

	application, err := app.NewWithOptions(app.Options{EnvFile: *envFile, AppHost: *appHost})
	if err != nil {
		logrus.WithError(err).Fatal("initialize app")
	}
	defer application.Close()
	// 应用生命周期独立于任何 HTTP 请求；退出时先取消重算和后台任务，再由 Close 回收数据库。
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := application.RunContext(ctx); err != nil {
		logging.LogTerminalError("run app", err)
		if closeErr := application.Close(); closeErr != nil {
			logrus.WithError(closeErr).Error("close app")
		}
		os.Exit(1)
	}
}
