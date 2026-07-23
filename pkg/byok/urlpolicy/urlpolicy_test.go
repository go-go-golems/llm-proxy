package urlpolicy

import "testing"

func TestNormalizeSecure(t *testing.T) {
	for _, test := range []struct {
		input string
		want  string
		ok    bool
	}{
		{input: "https://idp.example/path/", want: "https://idp.example/path", ok: true},
		{input: "http://127.0.0.1:8080/", want: "http://127.0.0.1:8080", ok: true},
		{input: "http://[::1]:8080/", want: "http://[::1]:8080", ok: true},
		{input: "http://idp.example", ok: false},
		{input: "https://user@idp.example", ok: false},
		{input: "https://idp.example?target=other", ok: false},
		{input: "relative", ok: false},
	} {
		t.Run(test.input, func(t *testing.T) {
			got, err := NormalizeSecure(test.input, true)
			if (err == nil) != test.ok || test.ok && got != test.want {
				t.Fatalf("NormalizeSecure() = %q, %v", got, err)
			}
		})
	}
}
