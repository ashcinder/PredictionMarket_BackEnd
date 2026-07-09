package main

import (
	"fmt"
	"os"

	"PredictionMarket/internal/injectpool"
)

func main() {
	if err := injectpool.RunCLI(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "injectpool: %v\n", err)
		os.Exit(1)
	}
}
