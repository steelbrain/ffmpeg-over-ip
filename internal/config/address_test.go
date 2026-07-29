package config

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestClientConfigAddresses(t *testing.T) {
	tests := []struct {
		name    string
		address string
		want    []string
	}{
		{"single", "192.168.1.100:5050", []string{"192.168.1.100:5050"}},
		{"pair", "a:5050,b:5050", []string{"a:5050", "b:5050"}},
		{"whitespace trimmed", " a:5050 , b:5050 ", []string{"a:5050", "b:5050"}},
		{"empty entries dropped", "a:5050,,b:5050,", []string{"a:5050", "b:5050"}},
		{"unix socket", "unix:/tmp/f.sock", []string{"unix:/tmp/f.sock"}},
		{"mixed tcp and unix", "unix:/tmp/f.sock,10.0.0.1:5050", []string{"unix:/tmp/f.sock", "10.0.0.1:5050"}},
		{"ipv6", "[fd46:d7ce:64eb::1]:5050,[::1]:5050", []string{"[fd46:d7ce:64eb::1]:5050", "[::1]:5050"}},
		{"only separators", ", ,", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &ClientConfig{Address: tt.address}
			got := cfg.Addresses()
			if len(got) == 0 && len(tt.want) == 0 {
				return
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("Addresses() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLoadClientConfigMultipleAddressesFromEnv(t *testing.T) {
	t.Setenv("FFMPEG_OVER_IP_CLIENT_CONFIG", "")
	t.Setenv("FFMPEG_OVER_IP_CLIENT_ADDRESS", "172.18.4.178:5050, 172.18.4.179:5050")
	t.Setenv("FFMPEG_OVER_IP_CLIENT_AUTH_SECRET", "secret")

	cfg, err := LoadClientConfig("")
	if err != nil {
		t.Fatalf("LoadClientConfig failed: %v", err)
	}
	want := []string{"172.18.4.178:5050", "172.18.4.179:5050"}
	if !slices.Equal(cfg.Addresses(), want) {
		t.Errorf("Addresses() = %q, want %q", cfg.Addresses(), want)
	}
}

// A non-empty address that splits to nothing must be rejected at load rather
// than surfacing later as a confusing "no server address configured" dial error.
func TestLoadClientConfigRejectsSeparatorOnlyAddress(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ffmpeg-over-ip.client.jsonc")
	if err := os.WriteFile(path, []byte(`{"address": " , ", "authSecret": "s"}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := LoadClientConfig(path); err == nil {
		t.Fatal("expected error for an address with no usable entries")
	}
}

func TestClientConfigDialTimeoutDuration(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  time.Duration
	}{
		{"unset uses default", "", DefaultDialTimeout},
		{"explicit", "1500ms", 1500 * time.Millisecond},
		{"seconds", "30s", 30 * time.Second},
		{"zero disables", "0", 0},
		{"garbage falls back", "soon", DefaultDialTimeout},
		{"negative falls back", "-5s", DefaultDialTimeout},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &ClientConfig{DialTimeout: tt.value}
			if got := cfg.DialTimeoutDuration(); got != tt.want {
				t.Errorf("DialTimeoutDuration() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestLoadClientConfigDialTimeoutFromEnv(t *testing.T) {
	t.Setenv("FFMPEG_OVER_IP_CLIENT_CONFIG", "")
	t.Setenv("FFMPEG_OVER_IP_CLIENT_ADDRESS", "a:5050")
	t.Setenv("FFMPEG_OVER_IP_CLIENT_AUTH_SECRET", "secret")
	t.Setenv("FFMPEG_OVER_IP_CLIENT_DIAL_TIMEOUT", "2s")

	cfg, err := LoadClientConfig("")
	if err != nil {
		t.Fatalf("LoadClientConfig failed: %v", err)
	}
	if got := cfg.DialTimeoutDuration(); got != 2*time.Second {
		t.Errorf("DialTimeoutDuration() = %s, want 2s", got)
	}
}
