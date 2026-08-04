package remote

import "strings"

// ShellQuote returns a POSIX shell-safe representation of one argument.
func ShellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
