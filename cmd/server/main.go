package main

import (
	"flag"
	"os"

	"cpa-usage-keeper/internal/app"
	"cpa-usage-keeper/internal/logging"
	"github.com/sirupsen/logrus"
)

func main() {
	logging.ConfigureBootstrap()

	envFile := flag.String("env", "", "path to env file")
	flag.Parse()

	application, err := app.NewWithOptions(app.Options{EnvFile: *envFile})
	if err != nil {
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
