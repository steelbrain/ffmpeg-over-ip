package common

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// rewritePath applies path rewriting rules to a string
// Unexported as it's only used internally by RewriteCommandArgs
func rewritePath(input string, rewrites [][2]string) string {
	result := input
	for _, rewrite := range rewrites {
		from, to := rewrite[0], rewrite[1]
		result = strings.Replace(result, from, to, -1)
	}
	return result
}

// RewriteCommandArgs rewrites all path-like arguments in a command
// This is used externally so must remain exported
func RewriteCommandArgs(args []string, rewrites [][2]string) []string {
	if len(rewrites) == 0 {
		return args
	}

	result := make([]string, len(args))
	for i, arg := range args {
		result[i] = rewritePath(arg, rewrites)
	}
	return result
}

// SetupLogger configures logging based on the configuration
// Returns a file handle for logging, or nil if logging is disabled
func SetupLogger(logConfig string) (*os.File, error) {
	// Handle special cases first
	switch logConfig {
	case "": // No logging
		return nil, nil
	case "stdout":
		return os.Stdout, nil
	case "stderr":
		return os.Stderr, nil
	}

	// Create directory if needed
	dir := filepath.Dir(logConfig)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create log directory: %w", err)
	}

	// Open log file
	f, err := os.OpenFile(logConfig, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %w", err)
	}

	return f, nil
}

// PrintConfigPaths prints out the valid configuration search paths
func PrintConfigPaths(paths []string) {
	fmt.Println("Configuration search paths (in order of preference):")
	for i, path := range paths {
		fmt.Printf("%d. %s\n", i+1, path)
	}
}
