package main

import (
	"log/slog"
	"memberos/internal/memberos"
	"os"
)

func main() {
	if err := memberos.Run(); err != nil {
		slog.Error("service stopped", "error", err)
		os.Exit(1)
	}
}
