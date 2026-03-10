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

	"github.com/tidwall/jsonc"
)

// LogValue is a string that also accepts JSON boolean false (meaning "disable logging").
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
	Log        LogValue `json:"log"`
	Address    string   `json:"address"`
	AuthSecret string   `json:"authSecret"`
}

// LoadServerConfig loads the server config. If explicitPath is non-empty, it
// loads from that path directly. Otherwise it searches env var and standard paths.
func LoadServerConfig(explicitPath string) (*ServerConfig, error) {
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

// LoadClientConfig loads the client config. If explicitPath is non-empty, it
// loads from that path directly. Otherwise it searches env var and standard paths.
func LoadClientConfig(explicitPath string) (*ClientConfig, error) {
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
	if cfg.AuthSecret == "" {
		return nil, fmt.Errorf("config: authSecret is required")
	}
	return &cfg, nil
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

// SearchPaths returns the list of paths searched for a config file of the given type.
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

// SetupLogging configures the global logger based on the log config value.
// Supported values: "stdout", "stderr", "" / false (discard), or a file path.
// File paths support $TMPDIR, $HOME, and $USER interpolation.
func SetupLogging(logValue LogValue) {
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
			return
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "cannot open log file %s: %v, logging to stderr\n", path, err)
			return
		}
		log.SetOutput(f)
	}
}

// expandLogVars expands allow-listed environment variables in a log path.
// Supports both ${VAR} (braced) and $VAR (bare) syntax. Bare $VAR only
// expands when followed by a non-identifier character or end of string,
// so $HOME expands but $HOMEDIR does not. Use ${HOME}dir for disambiguation.
func expandLogVars(s string) string {
	for _, key := range []string{"TMPDIR", "HOME", "USER", "PWD"} {
		val := resolveVar(key)
		if val == "" {
			continue
		}
		// Expand ${VAR} first (unambiguous, no boundary check needed)
		s = strings.ReplaceAll(s, "${"+key+"}", val)
		// Then expand bare $VAR with word boundary check
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

// resolveVar returns the value for an allow-listed variable using OS APIs.
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

// ParseAddress returns the network type and address from a config address string.
// Addresses prefixed with "unix:" are treated as Unix domain sockets (the prefix
// is stripped). All other addresses are treated as TCP.
func ParseAddress(address string) (network, addr string) {
	if after, ok := strings.CutPrefix(address, "unix:"); ok {
		return "unix", after
	}
	return "tcp", address
}

// readJSONC reads a file, strips JSONC comments and trailing commas, and returns clean JSON bytes.
func readJSONC(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return jsonc.ToJSON(data), nil
}
