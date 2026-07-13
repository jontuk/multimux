package cmd

import (
	"slices"
	"testing"
)

func TestComputeOrigins(t *testing.T) {
	cases := []struct {
		name        string
		names       []string
		port        int
		behindProxy bool
		want        []string
	}{
		{
			name:  "direct TLS on custom port",
			names: []string{"mybox", "mybox.local"}, port: 8686,
			want: []string{"https://mybox:8686", "https://mybox.local:8686"},
		},
		{
			// Browsers omit a default ":443" from the Origin header.
			name:  "direct TLS on 443 uses portless origins",
			names: []string{"mux.example.com"}, port: 443,
			want: []string{"https://mux.example.com"},
		},
		{
			// Behind Caddy/Tailscale Serve the public origin is portless; the
			// explicit-port form stays for proxies published on odd ports.
			name:  "behind proxy adds portless origins",
			names: []string{"mux.example.com"}, port: 8686, behindProxy: true,
			want: []string{"https://mux.example.com:8686", "https://mux.example.com"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := computeOrigins(tc.names, tc.port, tc.behindProxy)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("computeOrigins(%v, %d, %v) = %v, want %v",
					tc.names, tc.port, tc.behindProxy, got, tc.want)
			}
		})
	}
}
