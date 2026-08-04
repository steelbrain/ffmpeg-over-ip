package config

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
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
		{"ipv6 with zone", "[fe80::1%lo0]:5050", []string{"[fe80::1%lo0]:5050"}},
		{"dns", "n100-1.local:5050,node2.example.com:5050", []string{"n100-1.local:5050", "node2.example.com:5050"}},
		{"dns and ipv6 mixed", "example.com:5050,[::1]:5050,n100.local:5050", []string{"example.com:5050", "[::1]:5050", "n100.local:5050"}},
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

func TestClientConfigDNSAndIPv6(t *testing.T) {
	// Ensure DNS and IPv6 parsing doesn't break Addresses()
	tests := []struct {
		name string
		addr string
	}{
		{"dns simple", "n100-1.local:5050"},
		{"dns fqdn", "transcode.example.com:5050"},
		{"ipv6 loopback", "[::1]:5050"},
		{"ipv6 with port", "[2001:db8::1]:5050"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &ClientConfig{Address: tt.addr}
			got := cfg.Addresses()
			if len(got) != 1 || got[0] != tt.addr {
				t.Errorf("Addresses() = %q, want [%q]", got, tt.addr)
			}
		})
	}
}
