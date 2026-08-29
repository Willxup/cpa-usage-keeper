package main

import (
	"flag"
	"fmt"
	"os"

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
		if app.IsInitializationErrorLogged(err) {
			os.Exit(1)
		}
		logrus.WithError(err).Fatal("initialize app")
	}
	defer application.Close()

	if err := application.Run(); err != nil {
		logging.LogTerminalError("run app", err)
		if closeErr := application.Close(); closeErr != nil {
			logrus.WithError(closeErr).Error("close app")
		}
		os.Exit(1)
	}
}
