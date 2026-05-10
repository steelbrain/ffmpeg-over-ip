// Package rewrite applies ordered string-replacement pairs to ffmpeg argv.
// Used by the server (server-side rewrites) and by the client (fallback
// rewrites when execing the local ffmpeg).
package rewrite

import "strings"

// Apply returns a copy of args with each rewrite pair applied in order via
// strings.ReplaceAll. If rewrites is empty, the input slice is returned as-is.
func Apply(args []string, rewrites [][2]string) []string {
	if len(rewrites) == 0 {
		return args
	}
	result := make([]string, len(args))
	for i, arg := range args {
		result[i] = arg
		for _, rw := range rewrites {
			result[i] = strings.ReplaceAll(result[i], rw[0], rw[1])
		}
	}
	return result
}
