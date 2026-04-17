package main

import (
	"log"

	"cpa-usage-keeper/internal/app"
)

func main() {
	application, err := app.New()
	if err != nil {
		log.Fatalf("initialize app: %v", err)
	}

	if err := application.Run(); err != nil {
		log.Fatalf("run app: %v", err)
	}
}
