package config

import (
	"bytes"
	"os"
)

// StripJSONCComments removes JavaScript-style comments from JSONC content.
// It properly handles:
// - Single-line comments (//)
// - Multi-line comments (/* */)
// - Comments within string literals (preserving them)
// - Escape sequences in strings
func StripJSONCComments(content []byte) []byte {
	var result bytes.Buffer
	inString := false            // Whether we're inside a string
	escaped := false             // Whether the current character is escaped
	inSingleLineComment := false // Whether we're in a // comment
	inMultiLineComment := false  // Whether we're in a /* */ comment
	inMultiLineCommentDepth := 0 // For nested /* */ patterns within comments

	for i := 0; i < len(content); i++ {
		c := content[i]

		if inSingleLineComment {
			// If we find a newline, end the comment
			if c == '\n' {
				inSingleLineComment = false
				result.WriteByte(c) // Keep the newline
			}
			continue
		}

		if inMultiLineComment {
			// Check for nested comment start (we'll ignore it but track for proper ending)
			if c == '/' && i+1 < len(content) && content[i+1] == '*' {
				inMultiLineCommentDepth++
				i++ // Skip the next character (*)
				continue
			}

			// Check for comment end marker
			if c == '*' && i+1 < len(content) && content[i+1] == '/' {
				if inMultiLineCommentDepth > 0 {
					// This is the end of a nested comment, just decrement the depth
					inMultiLineCommentDepth--
				} else {
					// This is the end of the actual comment
					inMultiLineComment = false
				}
				i++ // Skip the next character (/)
			}
			continue
		}

		if escaped {
			// If we're in an escaped state, just output the character and continue
			result.WriteByte(c)
			escaped = false
			continue
		}

		if inString {
			// In a string, check for escape sequences and closing quotes
			if c == '\\' {
				escaped = true
			} else if c == '"' {
				inString = false
			}
			result.WriteByte(c)
			continue
		}

		// We're not in a string, comment, or escape sequence
		if c == '"' {
			// Start of a string
			inString = true
			result.WriteByte(c)
		} else if c == '/' && i+1 < len(content) {
			// Check for the start of comments
			nextChar := content[i+1]
			if nextChar == '/' {
				// Start of a single-line comment
				inSingleLineComment = true
				i++ // Skip the next character (/)
			} else if nextChar == '*' {
				// Start of a multi-line comment
				inMultiLineComment = true
				i++ // Skip the next character (*)
			} else {
				// Just a division operator, not a comment
				result.WriteByte(c)
			}
		} else {
			// Regular character, just output it
			result.WriteByte(c)
		}
	}

	return result.Bytes()
}

// LoadJSONCFile loads and parses a JSONC file by first stripping comments
func LoadJSONCFile(path string) ([]byte, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	return StripJSONCComments(content), nil
}
