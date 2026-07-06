package web

import "testing"

func TestSanitizeReturnToAllowsOnlyLocalAbsolutePaths(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "empty", raw: "", want: "/app"},
		{name: "relative", raw: "app", want: "/app"},
		{name: "absolute url", raw: "https://evil.example/app", want: "/app"},
		{name: "protocol relative", raw: "//evil.example/app", want: "/app"},
		{name: "slash backslash", raw: "/\\evil.example/app", want: "/app"},
		{name: "embedded backslash", raw: "/app\\evil", want: "/app"},
		{name: "normal app path", raw: "/app/tokens?tab=usage", want: "/app/tokens?tab=usage"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sanitizeReturnTo(tt.raw); got != tt.want {
				t.Fatalf("sanitizeReturnTo(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}
