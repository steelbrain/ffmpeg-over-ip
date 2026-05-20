// Package rewrite applies ordered argv-element rewrite pairs to ffmpeg argv.
// Used by the server (server-side rewrites) and by the client (fallback
// rewrites when execing the local ffmpeg).
package rewrite

import (
	"slices"
	"strings"
)

// Apply returns a copy of args with each rewrite pair applied in order.
//
// Each rewrite is [find, replace]. Both are split on whitespace via
// strings.Fields, giving a sequence of tokens. A rewrite matches when the
// find tokens equal a consecutive run of whole argv elements; the matched
// run is replaced by the replace tokens (which may be longer, shorter, or
// empty — an empty or whitespace-only replace removes the matched run).
// Substring matching within an argv element is not supported — patterns
// must align on argv boundaries.
//
// Examples:
//
//	{"h264_nvenc", "h264_qsv"}            // 1→1: swap one element
//	{"-hwaccel qsv", "-hwaccel cuda"}     // 2→2: swap a two-element run
//	{"-hwaccel qsv",                      // 2→4: expand a two-element run
//	 "-hwaccel cuda -hwaccel_output_format cuda"}
//	{"-nostdin", ""}                       // 1→0: remove an element
//
// Rewrites are applied sequentially, so a later rewrite can match tokens
// produced by an earlier one.
//
// If rewrites is empty, the input slice is returned as-is.
func Apply(args []string, rewrites [][2]string) []string {
	if len(rewrites) == 0 {
		return args
	}
	out := append([]string(nil), args...)
	for _, rw := range rewrites {
		find := strings.Fields(rw[0])
		if len(find) == 0 {
			continue
		}
		repl := strings.Fields(rw[1])
		out = applyOne(out, find, repl)
	}
	return out
}

// applyOne scans args once, replacing every non-overlapping run of elements
// equal to find with repl. Scanning resumes past the spliced-in repl, so a
// rewrite that produces tokens matching its own find won't loop.
func applyOne(args, find, repl []string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); {
		if i+len(find) <= len(args) && slices.Equal(args[i:i+len(find)], find) {
			out = append(out, repl...)
			i += len(find)
			continue
		}
		out = append(out, args[i])
		i++
	}
	return out
}
