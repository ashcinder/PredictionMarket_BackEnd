package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"predictionmarket-simulator/internal/config"
	"predictionmarket-simulator/internal/sim"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to simulator config.yaml")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "simulator: %v\n", err)
		os.Exit(1)
	}
	logger := log.New(os.Stdout, "", 0)
	if err := sim.New(cfg, logger).Run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "simulator: %v\n", err)
		os.Exit(1)
	}
}
