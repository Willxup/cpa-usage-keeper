package main

import (
	"log"
	"os"

	"cpa-usage-keeper/internal/app"
)

func main() {
	application, err := app.New()
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
