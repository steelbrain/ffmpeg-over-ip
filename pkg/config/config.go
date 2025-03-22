package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ClientConfig represents the client configuration
type ClientConfig struct {
	Log        interface{} `json:"log"`
	Address    string      `json:"address"`
	AuthSecret string      `json:"authSecret"`
}

// ServerConfig represents the server configuration
type ServerConfig struct {
	Log        interface{} `json:"log"`
	Address    string      `json:"address"`
	AuthSecret string      `json:"authSecret"`
	FFmpegPath string      `json:"ffmpegPath"`
	Rewrites   [][2]string `json:"rewrites"`
	Debug      bool        `json:"debug"`
}

// LogConfig returns a string representing where to log or empty string for no logging
func ParseLogConfig(logConfig interface{}) (string, error) {
	if logConfig == nil || logConfig == false {
		return "", nil
	}

	if logStr, ok := logConfig.(string); ok {
		if logStr == "stdout" || logStr == "stderr" {
			return logStr, nil
		}

		// Allowed environment variables for expansion
		allowedEnvVars := []string{
			"HOME",
			"TMPDIR",
			"TMP",
			"TEMP",
			"USER",
			"LOGDIR",
			"PWD",
			"XDG_DATA_HOME",
			"XDG_CONFIG_HOME",
			"XDG_STATE_HOME",
		}

		// Expand environment variables in log path
		if strings.Contains(logStr, "$") {
			// First find all potential environment variables in the string
			for _, envVar := range allowedEnvVars {
				// Check both $VAR and ${VAR} formats
				plainVar := "$" + envVar
				braceVar := "${" + envVar + "}"

				if strings.Contains(logStr, plainVar) || strings.Contains(logStr, braceVar) {
					envValue := os.Getenv(envVar)
					// Replace both formats
					logStr = strings.Replace(logStr, plainVar, envValue, -1)
					logStr = strings.Replace(logStr, braceVar, envValue, -1)
				}
			}
		}

		return logStr, nil
	}

	return "", fmt.Errorf("invalid log configuration type: %T", logConfig)
}

// GetClientConfigPaths returns common paths where client config could be located
func GetClientConfigPaths() []string {
	return getConfigPaths("client")
}

// GetServerConfigPaths returns common paths where server config could be located
func GetServerConfigPaths() []string {
	return getConfigPaths("server")
}

// getConfigPaths returns common paths for the given config type
func getConfigPaths(configType string) []string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = ""
	}

	exePath, err := os.Executable()
	if err != nil {
		exePath = ""
	} else {
		exePath = filepath.Dir(exePath)
	}

	currentDir, err := os.Getwd()
	if err != nil {
		currentDir = ""
	}

	// Environment variable for explicit config path
	configEnv := os.Getenv(fmt.Sprintf("FFMPEG_OVER_IP_%s_CONFIG", strings.ToUpper(configType)))

	paths := []string{}

	// If explicitly set via env var, prioritize that
	if configEnv != "" {
		paths = append(paths, configEnv)
	}

	// Add standard search paths
	if currentDir != "" {
		paths = append(paths, filepath.Join(currentDir, fmt.Sprintf("ffmpeg-over-ip.%s.jsonc", configType)))
		paths = append(paths, filepath.Join(currentDir, fmt.Sprintf(".ffmpeg-over-ip.%s.jsonc", configType)))
	}

	if homeDir != "" {
		paths = append(paths, filepath.Join(homeDir, fmt.Sprintf(".ffmpeg-over-ip.%s.jsonc", configType)))
		paths = append(paths, filepath.Join(homeDir, ".config", fmt.Sprintf("ffmpeg-over-ip.%s.jsonc", configType)))
	}

	if exePath != "" && exePath != currentDir {
		paths = append(paths, filepath.Join(exePath, fmt.Sprintf("ffmpeg-over-ip.%s.jsonc", configType)))
		paths = append(paths, filepath.Join(exePath, fmt.Sprintf(".ffmpeg-over-ip.%s.jsonc", configType)))
	}

	// Standard system paths
	paths = append(paths, filepath.Join("/etc", fmt.Sprintf("ffmpeg-over-ip.%s.jsonc", configType)))
	paths = append(paths, filepath.Join("/usr/local/etc", fmt.Sprintf("ffmpeg-over-ip.%s.jsonc", configType)))

	return paths
}

// LoadClientConfig loads the client configuration from the first valid path
func LoadClientConfig(configPaths []string) (*ClientConfig, string, error) {
	for _, path := range configPaths {
		config, err := loadClientConfigFromPath(path)
		if err == nil {
			return config, path, nil
		}
		if !os.IsNotExist(err) {
			return nil, path, fmt.Errorf("error loading config from %s: %w", path, err)
		}
	}
	return nil, "", fmt.Errorf("no valid configuration found")
}

// LoadServerConfig loads the server configuration from the first valid path
func LoadServerConfig(configPaths []string) (*ServerConfig, string, error) {
	for _, path := range configPaths {
		config, err := loadServerConfigFromPath(path)
		if err == nil {
			return config, path, nil
		}
		if !os.IsNotExist(err) {
			return nil, path, fmt.Errorf("error loading config from %s: %w", path, err)
		}
	}
	return nil, "", fmt.Errorf("no valid configuration found")
}

// loadClientConfigFromPath loads client configuration from the specified path
func loadClientConfigFromPath(path string) (*ClientConfig, error) {
	data, err := LoadJSONCFile(path)
	if err != nil {
		return nil, err
	}

	var config ClientConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("error parsing JSON: %w", err)
	}

	return &config, nil
}

// loadServerConfigFromPath loads server configuration from the specified path
func loadServerConfigFromPath(path string) (*ServerConfig, error) {
	data, err := LoadJSONCFile(path)
	if err != nil {
		return nil, err
	}

	var config ServerConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("error parsing JSON: %w", err)
	}

	return &config, nil
}

// LoadConfigFromPath loads client or server config from a specific path
// It returns either a ClientConfig or ServerConfig depending on isClient parameter
func LoadConfigFromPath(path string, isClient bool) (interface{}, error) {
	// Reuse the same code path for both client and server configs
	data, err := LoadJSONCFile(path)
	if err != nil {
		return nil, err
	}

	if isClient {
		var config ClientConfig
		if err := json.Unmarshal(data, &config); err != nil {
			return nil, fmt.Errorf("error parsing JSON: %w", err)
		}
		return &config, nil
	}

	var config ServerConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("error parsing JSON: %w", err)
	}
	return &config, nil
}
