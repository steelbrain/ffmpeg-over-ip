package config

import (
	"bytes"
	"log"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadServerConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.jsonc")
	os.WriteFile(path, []byte(`{
		// server config
		"log": "stdout",
		"address": "0.0.0.0:5050",
		"authSecret": "test-secret",
		"rewrites": [["h264_nvenc", "h264_qsv"]]
	}`), 0o644)

	cfg, err := LoadServerConfig(path)
	if err != nil {
		t.Fatalf("LoadServerConfig failed: %v", err)
	}
	if cfg.Log != "stdout" {
		t.Errorf("Log = %q, want %q", cfg.Log, "stdout")
	}
	if cfg.Address != "0.0.0.0:5050" {
		t.Errorf("Address = %q, want %q", cfg.Address, "0.0.0.0:5050")
	}
	if cfg.AuthSecret != "test-secret" {
		t.Errorf("AuthSecret = %q, want %q", cfg.AuthSecret, "test-secret")
	}
	if len(cfg.Rewrites) != 1 || cfg.Rewrites[0][0] != "h264_nvenc" || cfg.Rewrites[0][1] != "h264_qsv" {
		t.Errorf("Rewrites = %v, want [[h264_nvenc h264_qsv]]", cfg.Rewrites)
	}
}

func TestLoadClientConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "client.jsonc")
	os.WriteFile(path, []byte(`{
		"log": "/tmp/client.log",
		"address": "192.168.1.100:5050",
		"authSecret": "my-secret"
	}`), 0o644)

	cfg, err := LoadClientConfig(path)
	if err != nil {
		t.Fatalf("LoadClientConfig failed: %v", err)
	}
	if cfg.Address != "192.168.1.100:5050" {
		t.Errorf("Address = %q, want %q", cfg.Address, "192.168.1.100:5050")
	}
	if cfg.AuthSecret != "my-secret" {
		t.Errorf("AuthSecret = %q, want %q", cfg.AuthSecret, "my-secret")
	}
}

func TestLoadConfigMissingFile(t *testing.T) {
	_, err := LoadServerConfig("/nonexistent/path.jsonc")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadConfigMalformedJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.jsonc")
	os.WriteFile(path, []byte(`{not valid json}`), 0o644)

	_, err := LoadServerConfig(path)
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

func TestLoadConfigEnvVar(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "env-config.jsonc")
	os.WriteFile(path, []byte(`{
		"address": "env-test:5050",
		"authSecret": "env-secret"
	}`), 0o644)

	t.Setenv("FFMPEG_OVER_IP_CLIENT_CONFIG", path)

	cfg, err := LoadClientConfig("")
	if err != nil {
		t.Fatalf("LoadClientConfig via env failed: %v", err)
	}
	if cfg.Address != "env-test:5050" {
		t.Errorf("Address = %q, want %q", cfg.Address, "env-test:5050")
	}
}

func TestLoadConfigSearchPath(t *testing.T) {
	dir := t.TempDir()

	// Write config to the "cwd" search path
	path := filepath.Join(dir, "ffmpeg-over-ip.server.jsonc")
	os.WriteFile(path, []byte(`{
		"address": "found:5050",
		"authSecret": "found-secret"
	}`), 0o644)

	// Change to the temp dir so search finds it
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	cfg, err := LoadServerConfig("")
	if err != nil {
		t.Fatalf("LoadServerConfig via search failed: %v", err)
	}
	if cfg.Address != "found:5050" {
		t.Errorf("Address = %q, want %q", cfg.Address, "found:5050")
	}
}

func TestLoadConfigNoFileFound(t *testing.T) {
	// Clear env var to prevent interference
	t.Setenv("FFMPEG_OVER_IP_SERVER_CONFIG", "")

	// Use a temp dir as cwd where no config exists
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	_, err := LoadServerConfig("")
	if err == nil {
		t.Fatal("expected error when no config found")
	}
}

func TestJSONCWithTrailingComma(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "trailing.jsonc")
	os.WriteFile(path, []byte(`{
		"address": "test:5050",
		"authSecret": "secret",
	}`), 0o644)

	cfg, err := LoadClientConfig(path)
	if err != nil {
		t.Fatalf("failed: %v", err)
	}
	if cfg.Address != "test:5050" {
		t.Errorf("Address = %q, want %q", cfg.Address, "test:5050")
	}
}

func TestServerConfigMinimalFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "minimal.jsonc")
	os.WriteFile(path, []byte(`{
		"address": "0.0.0.0:5050",
		"authSecret": "secret"
	}`), 0o644)

	cfg, err := LoadServerConfig(path)
	if err != nil {
		t.Fatalf("LoadServerConfig failed: %v", err)
	}
	if cfg.Log != "" {
		t.Errorf("Log = %q, want empty string", cfg.Log)
	}
	if cfg.Address != "0.0.0.0:5050" {
		t.Errorf("Address = %q, want %q", cfg.Address, "0.0.0.0:5050")
	}
	if cfg.AuthSecret != "secret" {
		t.Errorf("AuthSecret = %q, want %q", cfg.AuthSecret, "secret")
	}
	if cfg.Rewrites != nil {
		t.Errorf("Rewrites = %v, want nil", cfg.Rewrites)
	}
}

func TestServerConfigMultipleRewrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rewrites.jsonc")
	os.WriteFile(path, []byte(`{
		"address": "0.0.0.0:5050",
		"authSecret": "secret",
		"rewrites": [
			["h264_nvenc", "h264_qsv"],
			["hevc_nvenc", "hevc_qsv"],
			["av1_nvenc", "av1_qsv"]
		]
	}`), 0o644)

	cfg, err := LoadServerConfig(path)
	if err != nil {
		t.Fatalf("LoadServerConfig failed: %v", err)
	}
	if len(cfg.Rewrites) != 3 {
		t.Fatalf("len(Rewrites) = %d, want 3", len(cfg.Rewrites))
	}
	expected := [][2]string{
		{"h264_nvenc", "h264_qsv"},
		{"hevc_nvenc", "hevc_qsv"},
		{"av1_nvenc", "av1_qsv"},
	}
	for i, pair := range expected {
		if cfg.Rewrites[i] != pair {
			t.Errorf("Rewrites[%d] = %v, want %v", i, cfg.Rewrites[i], pair)
		}
	}
}

func TestServerConfigEmptyRewrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty-rewrites.jsonc")
	os.WriteFile(path, []byte(`{
		"address": "0.0.0.0:5050",
		"authSecret": "secret",
		"rewrites": []
	}`), 0o644)

	cfg, err := LoadServerConfig(path)
	if err != nil {
		t.Fatalf("LoadServerConfig failed: %v", err)
	}
	if len(cfg.Rewrites) != 0 {
		t.Errorf("len(Rewrites) = %d, want 0", len(cfg.Rewrites))
	}
	if cfg.Rewrites == nil {
		t.Error("Rewrites is nil, want empty slice")
	}
}

func TestClientConfigMinimalFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "minimal-client.jsonc")
	os.WriteFile(path, []byte(`{
		"address": "10.0.0.1:5050",
		"authSecret": "client-secret"
	}`), 0o644)

	cfg, err := LoadClientConfig(path)
	if err != nil {
		t.Fatalf("LoadClientConfig failed: %v", err)
	}
	if cfg.Log != "" {
		t.Errorf("Log = %q, want empty string", cfg.Log)
	}
	if cfg.Address != "10.0.0.1:5050" {
		t.Errorf("Address = %q, want %q", cfg.Address, "10.0.0.1:5050")
	}
	if cfg.AuthSecret != "client-secret" {
		t.Errorf("AuthSecret = %q, want %q", cfg.AuthSecret, "client-secret")
	}
}

func TestLoadServerConfigEnvVar(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "env-server.jsonc")
	os.WriteFile(path, []byte(`{
		"address": "env-server:5050",
		"authSecret": "env-server-secret",
		"log": "stderr"
	}`), 0o644)

	t.Setenv("FFMPEG_OVER_IP_SERVER_CONFIG", path)

	cfg, err := LoadServerConfig("")
	if err != nil {
		t.Fatalf("LoadServerConfig via env failed: %v", err)
	}
	if cfg.Address != "env-server:5050" {
		t.Errorf("Address = %q, want %q", cfg.Address, "env-server:5050")
	}
	if cfg.AuthSecret != "env-server-secret" {
		t.Errorf("AuthSecret = %q, want %q", cfg.AuthSecret, "env-server-secret")
	}
	if cfg.Log != "stderr" {
		t.Errorf("Log = %q, want %q", cfg.Log, "stderr")
	}
}

func TestLoadConfigHiddenFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".ffmpeg-over-ip.server.jsonc")
	os.WriteFile(path, []byte(`{
		"address": "hidden:5050",
		"authSecret": "hidden-secret"
	}`), 0o644)

	// Clear env var to prevent interference
	t.Setenv("FFMPEG_OVER_IP_SERVER_CONFIG", "")

	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	cfg, err := LoadServerConfig("")
	if err != nil {
		t.Fatalf("LoadServerConfig via hidden file failed: %v", err)
	}
	if cfg.Address != "hidden:5050" {
		t.Errorf("Address = %q, want %q", cfg.Address, "hidden:5050")
	}
	if cfg.AuthSecret != "hidden-secret" {
		t.Errorf("AuthSecret = %q, want %q", cfg.AuthSecret, "hidden-secret")
	}
}

func TestServerConfigExtraFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "extra.jsonc")
	os.WriteFile(path, []byte(`{
		"address": "0.0.0.0:5050",
		"authSecret": "secret",
		"unknownField": "should be ignored",
		"anotherExtra": 42
	}`), 0o644)

	cfg, err := LoadServerConfig(path)
	if err != nil {
		t.Fatalf("LoadServerConfig failed: %v", err)
	}
	if cfg.Address != "0.0.0.0:5050" {
		t.Errorf("Address = %q, want %q", cfg.Address, "0.0.0.0:5050")
	}
	if cfg.AuthSecret != "secret" {
		t.Errorf("AuthSecret = %q, want %q", cfg.AuthSecret, "secret")
	}
}

func TestLoadServerConfigEmptyJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.jsonc")
	os.WriteFile(path, []byte(`{}`), 0o644)

	_, err := LoadServerConfig(path)
	if err == nil {
		t.Fatal("expected error for empty config (missing required fields)")
	}
	if !strings.Contains(err.Error(), "address is required") {
		t.Errorf("error = %q, want mention of address", err.Error())
	}
}

func TestLoadClientConfigEmptyJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty-client.jsonc")
	os.WriteFile(path, []byte(`{}`), 0o644)

	_, err := LoadClientConfig(path)
	if err == nil {
		t.Fatal("expected error for empty config (missing required fields)")
	}
	if !strings.Contains(err.Error(), "address is required") {
		t.Errorf("error = %q, want mention of address", err.Error())
	}
}

func TestLoadServerConfigUnicodeValues(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "unicode.jsonc")
	os.WriteFile(path, []byte(`{
		"address": "서버:5050",
		"authSecret": "密码🔑"
	}`), 0o644)

	cfg, err := LoadServerConfig(path)
	if err != nil {
		t.Fatalf("LoadServerConfig failed: %v", err)
	}
	if cfg.Address != "서버:5050" {
		t.Errorf("Address = %q, want %q", cfg.Address, "서버:5050")
	}
	if cfg.AuthSecret != "密码🔑" {
		t.Errorf("AuthSecret = %q, want %q", cfg.AuthSecret, "密码🔑")
	}
}

func TestSearchPathsOrder(t *testing.T) {
	// Create two directories: one for env var config, one for CWD config
	envDir := t.TempDir()
	cwdDir := t.TempDir()

	envPath := filepath.Join(envDir, "env-priority.jsonc")
	os.WriteFile(envPath, []byte(`{
		"address": "env-wins:5050",
		"authSecret": "env-secret"
	}`), 0o644)

	cwdPath := filepath.Join(cwdDir, "ffmpeg-over-ip.server.jsonc")
	os.WriteFile(cwdPath, []byte(`{
		"address": "cwd-loses:5050",
		"authSecret": "cwd-secret"
	}`), 0o644)

	t.Setenv("FFMPEG_OVER_IP_SERVER_CONFIG", envPath)

	origDir, _ := os.Getwd()
	os.Chdir(cwdDir)
	defer os.Chdir(origDir)

	cfg, err := LoadServerConfig("")
	if err != nil {
		t.Fatalf("LoadServerConfig failed: %v", err)
	}
	if cfg.Address != "env-wins:5050" {
		t.Errorf("Address = %q, want %q (env var should take priority over CWD)", cfg.Address, "env-wins:5050")
	}
}


func TestLoadServerConfigAllFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "full.jsonc")
	os.WriteFile(path, []byte(`{
		"log": "/var/log/ffmpeg-over-ip.log",
		"address": "10.0.0.5:9090",
		"authSecret": "super-secret-key",
		"rewrites": [
			["h264_nvenc", "h264_qsv"],
			["hevc_nvenc", "hevc_amf"],
			["av1_nvenc", "libsvtav1"]
		]
	}`), 0o644)

	cfg, err := LoadServerConfig(path)
	if err != nil {
		t.Fatalf("LoadServerConfig failed: %v", err)
	}
	if cfg.Log != "/var/log/ffmpeg-over-ip.log" {
		t.Errorf("Log = %q, want %q", cfg.Log, "/var/log/ffmpeg-over-ip.log")
	}
	if cfg.Address != "10.0.0.5:9090" {
		t.Errorf("Address = %q, want %q", cfg.Address, "10.0.0.5:9090")
	}
	if cfg.AuthSecret != "super-secret-key" {
		t.Errorf("AuthSecret = %q, want %q", cfg.AuthSecret, "super-secret-key")
	}
	if len(cfg.Rewrites) != 3 {
		t.Fatalf("len(Rewrites) = %d, want 3", len(cfg.Rewrites))
	}
	expected := [][2]string{
		{"h264_nvenc", "h264_qsv"},
		{"hevc_nvenc", "hevc_amf"},
		{"av1_nvenc", "libsvtav1"},
	}
	for i, pair := range expected {
		if cfg.Rewrites[i] != pair {
			t.Errorf("Rewrites[%d] = %v, want %v", i, cfg.Rewrites[i], pair)
		}
	}
}

func TestLoadClientConfigWithLog(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "client-log.jsonc")
	os.WriteFile(path, []byte(`{
		"log": "/tmp/client-debug.log",
		"address": "192.168.1.50:5050",
		"authSecret": "client-key"
	}`), 0o644)

	cfg, err := LoadClientConfig(path)
	if err != nil {
		t.Fatalf("LoadClientConfig failed: %v", err)
	}
	if cfg.Log != "/tmp/client-debug.log" {
		t.Errorf("Log = %q, want %q", cfg.Log, "/tmp/client-debug.log")
	}
	if cfg.Address != "192.168.1.50:5050" {
		t.Errorf("Address = %q, want %q", cfg.Address, "192.168.1.50:5050")
	}
	if cfg.AuthSecret != "client-key" {
		t.Errorf("AuthSecret = %q, want %q", cfg.AuthSecret, "client-key")
	}
}

func TestLoadConfigUnreadableFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "unreadable.jsonc")
	os.WriteFile(path, []byte(`{"address":"x:1","authSecret":"s"}`), 0o644)
	os.Chmod(path, 0o000)
	defer os.Chmod(path, 0o644) // restore for cleanup

	_, err := LoadServerConfig(path)
	if err == nil {
		t.Fatal("expected error for unreadable config file")
	}
}

func TestLoadServerConfigMissingAddress(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "no-addr.jsonc")
	os.WriteFile(path, []byte(`{"authSecret": "secret"}`), 0o644)

	_, err := LoadServerConfig(path)
	if err == nil {
		t.Fatal("expected error for missing address")
	}
	if !strings.Contains(err.Error(), "address is required") {
		t.Errorf("error = %q, want mention of address", err.Error())
	}
}

func TestLoadServerConfigMissingAuthSecret(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "no-auth.jsonc")
	os.WriteFile(path, []byte(`{"address": "0.0.0.0:5050"}`), 0o644)

	_, err := LoadServerConfig(path)
	if err == nil {
		t.Fatal("expected error for missing authSecret")
	}
	if !strings.Contains(err.Error(), "authSecret is required") {
		t.Errorf("error = %q, want mention of authSecret", err.Error())
	}
}

func TestLoadClientConfigMissingAddress(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "no-addr.jsonc")
	os.WriteFile(path, []byte(`{"authSecret": "secret"}`), 0o644)

	_, err := LoadClientConfig(path)
	if err == nil {
		t.Fatal("expected error for missing address")
	}
}

func TestLoadClientConfigMissingAuthSecret(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "no-auth.jsonc")
	os.WriteFile(path, []byte(`{"address": "0.0.0.0:5050"}`), 0o644)

	_, err := LoadClientConfig(path)
	if err == nil {
		t.Fatal("expected error for missing authSecret")
	}
}

func TestLoadServerConfigEndToEndCommentsAndTrailingCommas(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "full.jsonc")
	os.WriteFile(path, []byte(`{
		// Server config
		"log": "stderr", // log to stderr
		"address": "0.0.0.0:9090", // custom port
		"authSecret": "super-secret",
		"rewrites": [
			["h264_nvenc", "h264_qsv"], // nvidia -> intel
			["hevc_nvenc", "hevc_qsv"],
		],
	}`), 0o644)

	cfg, err := LoadServerConfig(path)
	if err != nil {
		t.Fatalf("LoadServerConfig failed: %v", err)
	}
	if cfg.Log != "stderr" {
		t.Errorf("Log = %q, want %q", cfg.Log, "stderr")
	}
	if cfg.Address != "0.0.0.0:9090" {
		t.Errorf("Address = %q, want %q", cfg.Address, "0.0.0.0:9090")
	}
	if cfg.AuthSecret != "super-secret" {
		t.Errorf("AuthSecret = %q, want %q", cfg.AuthSecret, "super-secret")
	}
	if len(cfg.Rewrites) != 2 {
		t.Fatalf("len(Rewrites) = %d, want 2", len(cfg.Rewrites))
	}
}

func TestLoadClientConfigEndToEndCommentsAndTrailingCommas(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "full.jsonc")
	os.WriteFile(path, []byte(`{
		// Client config
		"log": false, // disable logging
		"address": "10.0.0.1:5050", // server address
		"authSecret": "my-secret", // must match server
	}`), 0o644)

	cfg, err := LoadClientConfig(path)
	if err != nil {
		t.Fatalf("LoadClientConfig failed: %v", err)
	}
	if cfg.Log != "" {
		t.Errorf("Log = %q, want empty (boolean false)", cfg.Log)
	}
	if cfg.Address != "10.0.0.1:5050" {
		t.Errorf("Address = %q, want %q", cfg.Address, "10.0.0.1:5050")
	}
}

func TestSetupLoggingStdout(t *testing.T) {
	defer log.SetOutput(os.Stderr)

	SetupLogging(LogValue("stdout"))
	log.Print("test-stdout-message")
}

func TestSetupLoggingStderr(t *testing.T) {
	defer log.SetOutput(os.Stderr)

	SetupLogging(LogValue("stderr"))
}

func TestSetupLoggingEmpty(t *testing.T) {
	defer log.SetOutput(os.Stderr)

	SetupLogging(LogValue(""))
	// Logging should be silenced — write and verify nothing panics
	log.Print("this should be discarded")
}

func TestLogValueBooleanFalse(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bool-false.jsonc")
	os.WriteFile(path, []byte(`{
		"log": false,
		"address": "0.0.0.0:5050",
		"authSecret": "secret"
	}`), 0o644)

	cfg, err := LoadServerConfig(path)
	if err != nil {
		t.Fatalf("LoadServerConfig failed: %v", err)
	}
	if cfg.Log != "" {
		t.Errorf("Log = %q, want empty (boolean false should disable logging)", cfg.Log)
	}
}

func TestLogValueStringFalseIsFilePath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "string-false.jsonc")
	os.WriteFile(path, []byte(`{
		"log": "false",
		"address": "0.0.0.0:5050",
		"authSecret": "secret"
	}`), 0o644)

	cfg, err := LoadServerConfig(path)
	if err != nil {
		t.Fatalf("LoadServerConfig failed: %v", err)
	}
	if cfg.Log != "false" {
		t.Errorf("Log = %q, want %q (string \"false\" should be treated as file path)", cfg.Log, "false")
	}
}

func TestLogValueRejectsOtherBooleans(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bool-true.jsonc")
	os.WriteFile(path, []byte(`{
		"log": true,
		"address": "0.0.0.0:5050",
		"authSecret": "secret"
	}`), 0o644)

	_, err := LoadServerConfig(path)
	if err == nil {
		t.Fatal("expected error for log: true")
	}
}

func TestSetupLoggingFilePath(t *testing.T) {
	defer log.SetOutput(os.Stderr)

	dir := t.TempDir()
	logPath := filepath.Join(dir, "test.log")

	SetupLogging(LogValue(logPath))
	log.Print("file-log-message")

	// Force flush by switching output
	log.SetOutput(os.Stderr)

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("reading log file: %v", err)
	}
	if !strings.Contains(string(data), "file-log-message") {
		t.Errorf("log file contents = %q, want to contain %q", string(data), "file-log-message")
	}
}

func TestSetupLoggingFileAppends(t *testing.T) {
	defer log.SetOutput(os.Stderr)

	dir := t.TempDir()
	logPath := filepath.Join(dir, "append.log")

	// Write initial content
	os.WriteFile(logPath, []byte("existing\n"), 0644)

	SetupLogging(LogValue(logPath))
	log.Print("appended-message")
	log.SetOutput(os.Stderr)

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("reading log file: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "existing") {
		t.Error("existing content was overwritten")
	}
	if !strings.Contains(content, "appended-message") {
		t.Error("new message was not appended")
	}
}

func TestSetupLoggingMissingDirWarnsStderr(t *testing.T) {
	defer log.SetOutput(os.Stderr)

	// Capture stderr
	r, w, _ := os.Pipe()
	oldStderr := os.Stderr
	os.Stderr = w

	SetupLogging(LogValue("/nonexistent/directory/test.log"))

	w.Close()
	os.Stderr = oldStderr

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	if !strings.Contains(output, "does not exist") {
		t.Errorf("stderr = %q, want warning about missing directory", output)
	}
}

func TestSetupLoggingUnwritableFileWarnsStderr(t *testing.T) {
	defer log.SetOutput(os.Stderr)

	dir := t.TempDir()
	logPath := filepath.Join(dir, "test.log")

	// Create read-only file
	os.WriteFile(logPath, []byte("locked"), 0444)
	// Make directory read-only so file can't be opened for write
	os.Chmod(logPath, 0444)

	// Capture stderr
	r, w, _ := os.Pipe()
	oldStderr := os.Stderr
	os.Stderr = w

	SetupLogging(LogValue(logPath))

	w.Close()
	os.Stderr = oldStderr

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	// On some systems root can write to read-only files, so only check if we got a warning
	if output != "" && !strings.Contains(output, "cannot open log file") {
		t.Errorf("stderr = %q, want warning about unwritable file", output)
	}
}

func TestSetupLoggingTMPDIR(t *testing.T) {
	defer log.SetOutput(os.Stderr)

	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)

	SetupLogging(LogValue("$TMPDIR/tmpdir-test.log"))
	log.Print("tmpdir-message")
	log.SetOutput(os.Stderr)

	logPath := filepath.Join(dir, "tmpdir-test.log")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("reading log file: %v", err)
	}
	if !strings.Contains(string(data), "tmpdir-message") {
		t.Errorf("log file contents = %q, want to contain %q", string(data), "tmpdir-message")
	}
}

func TestSetupLoggingHOME(t *testing.T) {
	defer log.SetOutput(os.Stderr)

	dir := t.TempDir()
	t.Setenv("HOME", dir)

	SetupLogging(LogValue("$HOME/home-test.log"))
	log.Print("home-message")
	log.SetOutput(os.Stderr)

	logPath := filepath.Join(dir, "home-test.log")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("reading log file: %v", err)
	}
	if !strings.Contains(string(data), "home-message") {
		t.Errorf("log file contents = %q, want to contain %q", string(data), "home-message")
	}
}

func TestSetupLoggingTMPDIRFallback(t *testing.T) {
	defer log.SetOutput(os.Stderr)

	t.Setenv("TMPDIR", "")

	// os.TempDir() still returns a valid dir, so $TMPDIR should resolve
	SetupLogging(LogValue("$TMPDIR/tmpdir-fallback-test.log"))
	log.Print("fallback-message")
	log.SetOutput(os.Stderr)

	logPath := filepath.Join(os.TempDir(), "tmpdir-fallback-test.log")
	defer os.Remove(logPath)

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("reading log file: %v", err)
	}
	if !strings.Contains(string(data), "fallback-message") {
		t.Errorf("log file contents = %q, want to contain %q", string(data), "fallback-message")
	}
}

func TestExpandLogVars(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		envs   map[string]string
		want   string
	}{
		{
			name:  "both vars",
			input: "$TMPDIR/logs/$HOME/app.log",
			envs:  map[string]string{"TMPDIR": "/tmp/test", "HOME": "/home/user"},
			want:  "/tmp/test/logs//home/user/app.log",
		},
		{
			name:  "no vars",
			input: "/var/log/app.log",
			envs:  map[string]string{"TMPDIR": "/tmp", "HOME": "/home/user"},
			want:  "/var/log/app.log",
		},
		{
			name:  "unknown var not expanded",
			input: "$FOOBAR/app.log",
			envs:  map[string]string{"FOOBAR": "/should/not/expand"},
			want:  "$FOOBAR/app.log",
		},
		{
			name:  "HOME only",
			input: "$HOME/app.log",
			envs:  map[string]string{"HOME": "/Users/test"},
			want:  "/Users/test/app.log",
		},
		{
			name:  "TMPDIR only",
			input: "$TMPDIR/app.log",
			envs:  map[string]string{"TMPDIR": "/private/tmp"},
			want:  "/private/tmp/app.log",
		},
		{
			name:  "HOME alone no trailing path",
			input: "$HOME",
			envs:  map[string]string{"HOME": "/Users/test"},
			want:  "/Users/test",
		},
		{
			name:  "TMPDIR alone no trailing path",
			input: "$TMPDIR",
			envs:  map[string]string{"TMPDIR": "/tmp"},
			want:  "/tmp",
		},
		{
			name:  "adjacent vars no separator",
			input: "$TMPDIR$HOME",
			envs:  map[string]string{"TMPDIR": "/tmp", "HOME": "/home"},
			want:  "/tmp/home",
		},
		{
			name:  "same var twice",
			input: "$TMPDIR/a/$TMPDIR/b",
			envs:  map[string]string{"TMPDIR": "/tmp"},
			want:  "/tmp/a//tmp/b",
		},
		{
			name:  "partial match HOME - $HOM not expanded",
			input: "$HOM/app.log",
			envs:  map[string]string{"HOME": "/Users/test"},
			want:  "$HOM/app.log",
		},
		{
			name:  "partial match TMPDIR - $TMP not expanded",
			input: "$TMP/app.log",
			envs:  map[string]string{"TMPDIR": "/tmp"},
			want:  "$TMP/app.log",
		},
		{
			name:  "superstring $TMPDIRX not expanded",
			input: "$TMPDIRX/app.log",
			envs:  map[string]string{"TMPDIR": "/tmp"},
			want:  "$TMPDIRX/app.log",
		},
		{
			name:  "superstring $HOMEDIR not expanded",
			input: "$HOMEDIR/app.log",
			envs:  map[string]string{"HOME": "/Users/test"},
			want:  "$HOMEDIR/app.log",
		},
		{
			name:  "$HOME_ not expanded (underscore is identifier char)",
			input: "$HOME_DIR/app.log",
			envs:  map[string]string{"HOME": "/Users/test"},
			want:  "$HOME_DIR/app.log",
		},
		{
			name:  "$TMPDIR followed by digit not expanded",
			input: "$TMPDIR2/app.log",
			envs:  map[string]string{"TMPDIR": "/tmp"},
			want:  "$TMPDIR2/app.log",
		},
		{
			name:  "$HOME followed by slash expands",
			input: "$HOME/app.log",
			envs:  map[string]string{"HOME": "/Users/test"},
			want:  "/Users/test/app.log",
		},
		{
			name:  "$HOME followed by backslash expands",
			input: "$HOME\\app.log",
			envs:  map[string]string{"HOME": "C:\\Users\\test"},
			want:  "C:\\Users\\test\\app.log",
		},
		{
			name:  "$HOME followed by dot expands",
			input: "$HOME.log",
			envs:  map[string]string{"HOME": "/Users/test"},
			want:  "/Users/test.log",
		},
		{
			name:  "$HOME followed by hyphen expands",
			input: "$HOME-old/app.log",
			envs:  map[string]string{"HOME": "/Users/test"},
			want:  "/Users/test-old/app.log",
		},
		{
			name:  "braced ${HOME}",
			input: "${HOME}/app.log",
			envs:  map[string]string{"HOME": "/Users/test"},
			want:  "/Users/test/app.log",
		},
		{
			name:  "braced ${TMPDIR}",
			input: "${TMPDIR}/app.log",
			envs:  map[string]string{"TMPDIR": "/tmp"},
			want:  "/tmp/app.log",
		},
		{
			name:  "braced disambiguates ${HOME}dir",
			input: "${HOME}dir/app.log",
			envs:  map[string]string{"HOME": "/Users/test"},
			want:  "/Users/testdir/app.log",
		},
		{
			name:  "braced disambiguates ${TMPDIR}extra",
			input: "${TMPDIR}extra/app.log",
			envs:  map[string]string{"TMPDIR": "/tmp"},
			want:  "/tmpextra/app.log",
		},
		{
			name:  "braced both vars",
			input: "${TMPDIR}/${HOME}/app.log",
			envs:  map[string]string{"TMPDIR": "/tmp", "HOME": "/home/user"},
			want:  "/tmp//home/user/app.log",
		},
		{
			name:  "braced HOME env empty falls back to user.Current",
			input: "${HOME}/app.log",
			envs:  map[string]string{"HOME": ""},
			want: func() string {
				u, _ := user.Current()
				return u.HomeDir
			}() + "/app.log",
		},
		{
			name:  "mixed braced and bare",
			input: "${HOME}/$TMPDIR/app.log",
			envs:  map[string]string{"HOME": "/home/user", "TMPDIR": "/tmp"},
			want:  "/home/user//tmp/app.log",
		},
		{
			name:  "$USER expands",
			input: "/var/log/$USER/app.log",
			envs:  nil,
			want:  "/var/log/" + resolveVar("USER") + "/app.log",
		},
		{
			name:  "${USER} braced expands",
			input: "/var/log/${USER}-ffmpeg.log",
			envs:  nil,
			want:  "/var/log/" + resolveVar("USER") + "-ffmpeg.log",
		},
		{
			name:  "$USERNAME not expanded (superstring)",
			input: "$USERNAME/app.log",
			envs:  nil,
			want:  "$USERNAME/app.log",
		},
		{
			name:  "$PWD expands",
			input: "$PWD/app.log",
			envs:  nil,
			want:  func() string { cwd, _ := os.Getwd(); return cwd }() + "/app.log",
		},
		{
			name:  "${PWD} braced expands",
			input: "${PWD}/app.log",
			envs:  nil,
			want:  func() string { cwd, _ := os.Getwd(); return cwd }() + "/app.log",
		},
		{
			name:  "HOME env empty falls back to user.Current",
			input: "$HOME/app.log",
			envs:  map[string]string{"HOME": ""},
			want: func() string {
				u, _ := user.Current()
				return u.HomeDir
			}() + "/app.log",
		},
		{
			name:  "TMPDIR env empty falls back to /tmp",
			input: "$TMPDIR/app.log",
			envs:  map[string]string{"TMPDIR": ""},
			want:  "/tmp/app.log",
		},
		{
			name:  "empty input",
			input: "",
			envs:  map[string]string{"TMPDIR": "/tmp", "HOME": "/home"},
			want:  "",
		},
		{
			name:  "dollar sign but not a var",
			input: "$100/app.log",
			envs:  map[string]string{"TMPDIR": "/tmp", "HOME": "/home"},
			want:  "$100/app.log",
		},
		{
			name:  "windows style path with HOME",
			input: "$HOME\\logs\\app.log",
			envs:  map[string]string{"HOME": "C:\\Users\\test"},
			want:  "C:\\Users\\test\\logs\\app.log",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.envs {
				t.Setenv(k, v)
			}
			got := expandLogVars(tt.input)
			if got != tt.want {
				t.Errorf("expandLogVars(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestServerConfigDebugFlag(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "debug.jsonc")
	os.WriteFile(path, []byte(`{
		"address": "0.0.0.0:5050",
		"authSecret": "secret",
		"debug": true
	}`), 0o644)

	cfg, err := LoadServerConfig(path)
	if err != nil {
		t.Fatalf("LoadServerConfig failed: %v", err)
	}
	if !cfg.Debug {
		t.Error("Debug = false, want true")
	}
}

func TestServerConfigDebugDefaultFalse(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "no-debug.jsonc")
	os.WriteFile(path, []byte(`{
		"address": "0.0.0.0:5050",
		"authSecret": "secret"
	}`), 0o644)

	cfg, err := LoadServerConfig(path)
	if err != nil {
		t.Fatalf("LoadServerConfig failed: %v", err)
	}
	if cfg.Debug {
		t.Error("Debug = true, want false (should default to false)")
	}
}

func TestParseAddress(t *testing.T) {
	tests := []struct {
		input       string
		wantNetwork string
		wantAddr    string
	}{
		// TCP — IP addresses
		{"0.0.0.0:5050", "tcp", "0.0.0.0:5050"},
		{"127.0.0.1:5050", "tcp", "127.0.0.1:5050"},
		{"192.168.1.100:5050", "tcp", "192.168.1.100:5050"},
		{"10.0.0.1:80", "tcp", "10.0.0.1:80"},
		// TCP — IPv6
		{"[::1]:5050", "tcp", "[::1]:5050"},
		{"[::]:5050", "tcp", "[::]:5050"},
		{"[fe80::1%25eth0]:5050", "tcp", "[fe80::1%25eth0]:5050"},
		// TCP — hostnames
		{"server.example.com:5050", "tcp", "server.example.com:5050"},
		{"localhost:5050", "tcp", "localhost:5050"},
		{"my-server:9090", "tcp", "my-server:9090"},
		// TCP — no host (all interfaces)
		{":5050", "tcp", ":5050"},
		// TCP — high port
		{"0.0.0.0:65535", "tcp", "0.0.0.0:65535"},
		// Unix — absolute paths
		{"unix:/tmp/ffmpeg-over-ip.sock", "unix", "/tmp/ffmpeg-over-ip.sock"},
		{"unix:/var/run/ffmpeg.sock", "unix", "/var/run/ffmpeg.sock"},
		{"unix:/a/b/c/d/e/f.sock", "unix", "/a/b/c/d/e/f.sock"},
		// Unix — relative paths
		{"unix:./ffmpeg.sock", "unix", "./ffmpeg.sock"},
		{"unix:../parent/ffmpeg.sock", "unix", "../parent/ffmpeg.sock"},
		{"unix:ffmpeg.sock", "unix", "ffmpeg.sock"},
		// Unix — Windows paths
		{"unix:C:\\tmp\\ffmpeg.sock", "unix", "C:\\tmp\\ffmpeg.sock"},
		{"unix:D:\\Program Files\\app\\sock", "unix", "D:\\Program Files\\app\\sock"},
		// Unix — paths with spaces
		{"unix:/tmp/my app/ffmpeg.sock", "unix", "/tmp/my app/ffmpeg.sock"},
		// Unix — empty path (degenerate but shouldn't panic)
		{"unix:", "unix", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			network, addr := ParseAddress(tt.input)
			if network != tt.wantNetwork {
				t.Errorf("ParseAddress(%q) network = %q, want %q", tt.input, network, tt.wantNetwork)
			}
			if addr != tt.wantAddr {
				t.Errorf("ParseAddress(%q) addr = %q, want %q", tt.input, addr, tt.wantAddr)
			}
		})
	}
}

