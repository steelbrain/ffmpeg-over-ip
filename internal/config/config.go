package config

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"github.com/tidwall/jsonc"
)

const DefaultDialTimeout = 2 * time.Second // default per-attempt dial timeout, small for fast failover

type LogValue string

func (l *LogValue) UnmarshalJSON(data []byte) error {
	if string(data) == "false" {
		*l = ""
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("log must be a string or false: %w", err)
	}
	*l = LogValue(s)
	return nil
}

type ServerConfig struct {
	Log        LogValue    `json:"log"`
	Address    string      `json:"address"`
	AuthSecret string      `json:"authSecret"`
	Rewrites   [][2]string `json:"rewrites"`
	Debug      bool        `json:"debug"`
}

type ClientConfig struct {
	Log LogValue `json:"log"`
	Address string `json:"address"`
	AuthSecret string `json:"authSecret"`
	// DialTimeout is per-attempt connect timeout. Go duration string with units:
	// "500ms" or "0.5s" = half second, "2s" = 2 seconds (default), "1m" = 1 minute.
	// Use small for fast failover across many little nodes.
	DialTimeout      string      `json:"dialTimeout"`
	FallbackToLocal  bool        `json:"fallbackToLocal"`
	FallbackRewrites [][2]string `json:"fallbackRewrites"`
	Debug            bool        `json:"debug"`
}

func (c *ClientConfig) Addresses() []string {
	out := []string{}
	for _, p := range strings.Split(c.Address, ",") {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func (c *ClientConfig) DialTimeoutDuration() time.Duration {
	if c.DialTimeout == "" {
		return DefaultDialTimeout
	}
	d, err := time.ParseDuration(c.DialTimeout)
	if err != nil || d < 0 {
		log.Printf("invalid dialTimeout %q, using %s", c.DialTimeout, DefaultDialTimeout)
		return DefaultDialTimeout
	}
	return d
}

func LoadServerConfig(explicitPath string) (*ServerConfig, error) {
	if explicitPath == "" && os.Getenv("FFMPEG_OVER_IP_SERVER_CONFIG") == "" {
		if cfg := serverConfigFromEnv(); cfg != nil {
			return cfg, nil
		}
	}
	data, err := loadConfigBytes(explicitPath, "server")
	if err != nil {
		return nil, err
	}
	var cfg ServerConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	if cfg.Address == "" {
		return nil, fmt.Errorf("config: address is required")
	}
	if cfg.AuthSecret == "" {
		return nil, fmt.Errorf("config: authSecret is required")
	}
	return &cfg, nil
}

func LoadClientConfig(explicitPath string) (*ClientConfig, error) {
	if explicitPath == "" && os.Getenv("FFMPEG_OVER_IP_CLIENT_CONFIG") == "" {
		if cfg := clientConfigFromEnv(); cfg != nil {
			return cfg, nil
		}
	}
	data, err := loadConfigBytes(explicitPath, "client")
	if err != nil {
		return nil, err
	}
	var cfg ClientConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	if cfg.Address == "" {
		return nil, fmt.Errorf("config: address is required")
	}
	if len(cfg.Addresses()) == 0 {
		return nil, fmt.Errorf("config: address %q contains no usable entries", cfg.Address)
	}
	if cfg.AuthSecret == "" {
		return nil, fmt.Errorf("config: authSecret is required")
	}
	return &cfg, nil
}

func serverConfigFromEnv() *ServerConfig {
	address := os.Getenv("FFMPEG_OVER_IP_SERVER_ADDRESS")
	authSecret := os.Getenv("FFMPEG_OVER_IP_SERVER_AUTH_SECRET")
	if address == "" || authSecret == "" {
		return nil
	}
	return &ServerConfig{
		Address:    address,
		AuthSecret: authSecret,
		Log:        LogValue(os.Getenv("FFMPEG_OVER_IP_SERVER_LOG")),
		Debug:      parseLaxBool(os.Getenv("FFMPEG_OVER_IP_SERVER_DEBUG")),
	}
}

func clientConfigFromEnv() *ClientConfig {
	address := os.Getenv("FFMPEG_OVER_IP_CLIENT_ADDRESS")
	authSecret := os.Getenv("FFMPEG_OVER_IP_CLIENT_AUTH_SECRET")
	if address == "" || authSecret == "" {
		return nil
	}
	return &ClientConfig{
		Address:         address,
		AuthSecret:      authSecret,
		Log:             LogValue(os.Getenv("FFMPEG_OVER_IP_CLIENT_LOG")),
		DialTimeout:     os.Getenv("FFMPEG_OVER_IP_CLIENT_DIAL_TIMEOUT"),
		FallbackToLocal: parseLaxBool(os.Getenv("FFMPEG_OVER_IP_CLIENT_FALLBACK_TO_LOCAL")),
		Debug:           parseLaxBool(os.Getenv("FFMPEG_OVER_IP_CLIENT_DEBUG")),
	}
}

func parseLaxBool(s string) bool {
	switch strings.ToLower(s) {
	case "true", "1", "yes", "y":
		return true
	default:
		return false
	}
}

func loadConfigBytes(explicitPath, configType string) ([]byte, error) {
	if explicitPath != "" {
		return readJSONC(explicitPath)
	}
	paths := searchPaths(configType)
	for _, p := range paths {
		data, err := readJSONC(p)
		if err == nil {
			return data, nil
		}
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("reading %s: %w", p, err)
		}
	}
	return nil, fmt.Errorf("no config file found (searched %d paths)", len(paths))
}

func SearchPaths(configType string) []string {
	return searchPaths(configType)
}

func searchPaths(configType string) []string {
	envKey := fmt.Sprintf("FFMPEG_OVER_IP_%s_CONFIG", strings.ToUpper(configType))
	filename := fmt.Sprintf("ffmpeg-over-ip.%s.jsonc", configType)
	hiddenFilename := "." + filename
	var paths []string
	if envPath := os.Getenv(envKey); envPath != "" {
		paths = append(paths, envPath)
	}
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		paths = append(paths, filepath.Join(exeDir, filename))
		paths = append(paths, filepath.Join(exeDir, hiddenFilename))
	}
	if cwd, err := os.Getwd(); err == nil {
		paths = append(paths, filepath.Join(cwd, filename))
		paths = append(paths, filepath.Join(cwd, hiddenFilename))
	}
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(home, hiddenFilename))
		paths = append(paths, filepath.Join(home, ".config", filename))
	}
	paths = append(paths, filepath.Join("/etc", filename))
	paths = append(paths, filepath.Join("/usr/local/etc", filename))
	return paths
}

func SetupLogging(logValue LogValue) func() {
	switch logValue {
	case "stdout":
		log.SetOutput(os.Stdout)
	case "stderr":
		log.SetOutput(os.Stderr)
	case "":
		log.SetOutput(io.Discard)
	default:
		path := expandLogVars(string(logValue))
		dir := filepath.Dir(path)
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			fmt.Fprintf(os.Stderr, "log directory %s does not exist, logging to stderr\n", dir)
			return func() {}
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "cannot open log file %s: %v, logging to stderr\n", path, err)
			return func() {}
		}
		log.SetOutput(f)
		return func() {
			log.SetOutput(io.Discard)
			f.Close()
		}
	}
	return func() {}
}

func expandLogVars(s string) string {
	for _, key := range []string{"TMPDIR", "HOME", "USER", "PWD"} {
		val := resolveVar(key)
		if val == "" {
			continue
		}
		s = strings.ReplaceAll(s, "${"+key+"}", val)
		token := "$" + key
		var result strings.Builder
		for {
			idx := strings.Index(s, token)
			if idx < 0 {
				result.WriteString(s)
				break
			}
			after := idx + len(token)
			if after < len(s) && isIdentChar(s[after]) {
				result.WriteString(s[:after])
				s = s[after:]
				continue
			}
			result.WriteString(s[:idx])
			result.WriteString(val)
			s = s[after:]
		}
		s = result.String()
	}
	return s
}

func isIdentChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_'
}

func resolveVar(key string) string {
	switch key {
	case "TMPDIR":
		return os.TempDir()
	case "HOME":
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			return home
		}
		if u, err := user.Current(); err == nil {
			return u.HomeDir
		}
	case "USER":
		if u, err := user.Current(); err == nil {
			return u.Username
		}
	case "PWD":
		if cwd, err := os.Getwd(); err == nil {
			return cwd
		}
	}
	return ""
}

func ParseAddress(address string) (network, addr string) {
	if after, ok := strings.CutPrefix(address, "unix:"); ok {
		return "unix", after
	}
	return "tcp", address
}

func readJSONC(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return jsonc.ToJSON(data), nil
}
