package remote

import "testing"

func TestShellQuote(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"":        "''",
		"service": "'service'",
		"a b":     "'a b'",
		"a'b":     "'a'\"'\"'b'",
	}
	for input, expected := range tests {
		if actual := ShellQuote(input); actual != expected {
			t.Errorf("ShellQuote(%q) = %q, want %q", input, actual, expected)
		}
	}
}
