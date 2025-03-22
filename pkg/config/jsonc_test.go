package config

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestStripJSONCComments(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "No comments",
			input:    `{"key": "value"}`,
			expected: `{"key": "value"}`,
		},
		{
			name:     "Single-line comment",
			input:    `{"key": "value"} // This is a comment`,
			expected: `{"key": "value"} `,
		},
		{
			name:     "Multi-line comment",
			input:    `{"key": /* inline comment */ "value"}`,
			expected: `{"key":  "value"}`,
		},
		{
			name: "Comment at the beginning",
			input: `// Header comment
{"key": "value"}`,
			expected: `
{"key": "value"}`,
		},
		{
			name:     "String with comment-like content",
			input:    `{"key": "This is not a // comment", "other": "Not /* a comment */ either"}`,
			expected: `{"key": "This is not a // comment", "other": "Not /* a comment */ either"}`,
		},
		{
			name:     "String with escaped quotes",
			input:    `{"key": "value with \"quotes\" inside // not a comment"}`,
			expected: `{"key": "value with \"quotes\" inside // not a comment"}`,
		},
		{
			name:     "Multi-line block comment spanning lines",
			input:    "{\n  \"key\": /* this comment\n  spans multiple\n  lines */ \"value\"\n}",
			expected: "{\n  \"key\":  \"value\"\n}",
		},
		{
			name:     "URL in string with double slashes",
			input:    `{"url": "https://example.com/path"}`,
			expected: `{"url": "https://example.com/path"}`,
		},
		{
			name:     "Windows file path with escaped backslashes",
			input:    `{"path": "C:\\\\Windows\\\\System32"} // Comment`,
			expected: `{"path": "C:\\\\Windows\\\\System32"} `,
		},
		{
			name: "Mix of comment styles with nested objects",
			input: `{
  "key1": "value1", // End of line comment
  /* Block comment */
  "key2": {
    // Nested comment
    "nested": "value" /* inline */
  }
}`,
			expected: `{
  "key1": "value1", 
  
  "key2": {
    
    "nested": "value" 
  }
}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Normalize expected by removing \n which is just for readability in the test cases
			expected := strings.ReplaceAll(tt.expected, "\\n", "\n")

			result := StripJSONCComments([]byte(tt.input))
			if string(result) != expected {
				t.Errorf("Expected:\n%s\nGot:\n%s", expected, string(result))
			}
		})
	}
}

// Embedded server JSONC content for testing
const embeddedServerJSONC = `{
  // This is a "jsonc" file and therefore supports comments in addition to standard JSON syntax

  // LOG CONFIGURATION OPTIONS (use only one):
  "log": "stdout", // type: "stdout" | "stderr" | any string (file path) | false
  // Other possibilities (EXAMPLES - choose only one):
  // "log": "$TMPDIR/ffmpeg-over-ip.server.log",  // Uses the operating system temp folder
  // "log": false,  // Turns off logging completely
  // "log": "stderr",  // Log to stderr
  // "log": "/var/log/messages.log",  // Log to a specific file

  "address": "0.0.0.0:5050", // type: string, format: "host:port" or "/path/to/unix.sock"
  // You can use either "host:port" format for TCP connections or a path to a Unix socket
  // Examples:
  // "address": "127.0.0.1:5050"    // Listen only on localhost
  // "address": "0.0.0.0:5050"     // Listen on all interfaces (default)
  // "address": "/tmp/ffmpeg-over-ip.sock"  // Use Unix socket

  "authSecret": "YOUR-CLIENT-PASSWORD-HERE", // type: string
  // ^ Ideally more than 15 characters long

  "ffmpegPath": "//opt/homebrew/bin/ffmpeg", // type: string
  // ^ For windows, you may have to use slash twice because of how strings in JSON work, so C:\Windows would be "C:\\Windows" etc

  "rewrites": [
  ]
}`

// TestLoadInvalidFile tests the behavior when loading non-existent files
func TestLoadInvalidFile(t *testing.T) {
	_, err := LoadJSONCFile("non-existent-file.jsonc")
	if err == nil {
		t.Error("Expected error when loading non-existent file, got nil")
	}
}

// TestEmbeddedServerJSONC tests parsing of the embedded server JSONC content
// This test doesn't rely on external files, making it more portable and reliable
func TestEmbeddedServerJSONC(t *testing.T) {
	// Process the embedded JSONC content
	stripped := StripJSONCComments([]byte(embeddedServerJSONC))

	// Verify we can parse the result as valid JSON
	var result map[string]interface{}
	err := json.Unmarshal(stripped, &result)
	if err != nil {
		t.Fatalf("Failed to parse embedded JSONC as JSON: %v\nContent:\n%s", err, string(stripped))
	}

	// Verify key existence for a few important keys
	requiredKeys := []string{"address", "authSecret", "ffmpegPath", "rewrites"}
	for _, key := range requiredKeys {
		if _, exists := result[key]; !exists {
			t.Errorf("Required key '%s' missing from parsed result", key)
		}
	}

	// Specifically verify the ffmpegPath that contains double-slashes
	ffmpegPath, ok := result["ffmpegPath"].(string)
	if !ok {
		t.Fatalf("ffmpegPath is not a string: %v", result["ffmpegPath"])
	}

	// Check that double-slashes are preserved correctly
	if ffmpegPath != "//opt/homebrew/bin/ffmpeg" {
		t.Errorf("ffmpegPath was not correctly parsed.\nExpected: //opt/homebrew/bin/ffmpeg\nGot: %s", ffmpegPath)
	}
}

// TestExtraTestCases looks at edge cases and particularly difficult patterns
func TestExtraTestCases(t *testing.T) {
	// Create a temporary test file with complex patterns
	complexContent := `{
		"string_with_comment_chars": "This string has // and /* */ sequences that should be preserved",
		"escaped_quotes": "Here are some \"quoted\" words with // comment markers",
		"url": "https://example.com/path",
		/* This is a comment
		   that spans multiple lines
		   and has "quotes" and nested /* comment-like */ sequences
		*/
		"windows_path": "C:\\\\Program Files\\\\App\\\\file.txt",
		// Another comment
		"trailing_comment": "value" // with a comment
	}`

	// Create temp file
	tempFile, err := os.CreateTemp("", "jsonc-test-*.jsonc")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tempFile.Name())

	_, err = tempFile.Write([]byte(complexContent))
	if err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	tempFile.Close()

	// Test our parser
	stripped, err := LoadJSONCFile(tempFile.Name())
	if err != nil {
		t.Fatalf("Failed to load complex test file: %v", err)
	}

	// Verify it can be parsed as JSON
	var result map[string]interface{}
	err = json.Unmarshal(stripped, &result)
	if err != nil {
		t.Fatalf("Failed to parse complex JSONC as JSON: %v\nContent:\n%s", err, string(stripped))
	}

	// Check that string values are preserved correctly
	expectedStrings := map[string]string{
		"string_with_comment_chars": "This string has // and /* */ sequences that should be preserved",
		"escaped_quotes":            "Here are some \"quoted\" words with // comment markers",
		"url":                       "https://example.com/path",
		"windows_path":              "C:\\\\Program Files\\\\App\\\\file.txt",
		"trailing_comment":          "value",
	}

	for key, expected := range expectedStrings {
		if actual, ok := result[key].(string); !ok || actual != expected {
			t.Errorf("Key '%s' not correctly preserved.\nExpected: %s\nGot: %v",
				key, expected, result[key])
		}
	}
}
