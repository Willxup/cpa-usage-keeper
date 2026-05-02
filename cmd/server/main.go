package main

import (
	"flag"
	"log"
	"os"

	"cpa-usage-keeper/internal/app"
)

func main() {
	envFile := flag.String("env", "", "path to env file")
	flag.Parse()

	application, err := app.NewWithOptions(app.Options{EnvFile: *envFile})
	if err != nil {
		log.Fatalf("initialize app: %v", err)
	}
	defer application.Close()

	if err := application.Run(); err != nil {
		log.Printf("run app: %v", err)
		if closeErr := application.Close(); closeErr != nil {
			log.Printf("close app: %v", closeErr)
		}
		os.Exit(1)
	}
}
