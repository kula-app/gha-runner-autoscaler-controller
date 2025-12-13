package main

import (
	"context"
	"fmt"
	"os"
)

func main() {
	// Entry point: create a root context and run the application.
	// We use context.Background() here because this is the top-level context for the application,
	// representing an empty context with no cancellation or values. It is the recommended starting
	// point for main functions, initialization, and tests, allowing child contexts to be derived
	// for request-scoped or cancelable operations as needed.
	ctx := context.Background()

	// Pass in the command line arguments, environment variables, and standard error
	// stream to the run function. This allows the run function to be tested in isolation
	// without relying on the command line or environment variables.
	if err := run(ctx, os.Args, os.Getenv, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}
}
