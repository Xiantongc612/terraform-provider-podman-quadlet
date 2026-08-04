// Package quadlet renders and inspects provider-managed Podman Quadlet files.
package quadlet

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

const header = "# Managed by terraform-provider-podlet\n# Format: 1\n"

// Pair is one key-value entry in a Quadlet section.
type Pair struct {
	Key   string
	Value string
}

// Section is an ordered Quadlet section.
type Section struct {
	Name  string
	Pairs []Pair
}

// Render creates deterministic content from ordered sections and pairs.
func Render(sections ...Section) []byte {
	var output strings.Builder
	output.WriteString(header)
	for _, section := range sections {
		output.WriteByte('\n')
		output.WriteByte('[')
		output.WriteString(section.Name)
		output.WriteString("]\n")
		for _, pair := range section.Pairs {
			output.WriteString(pair.Key)
			output.WriteByte('=')
			output.WriteString(pair.Value)
			output.WriteByte('\n')
		}
	}
	return []byte(output.String())
}

// Managed reports whether content carries this provider's ownership marker.
func Managed(content []byte) bool {
	return strings.HasPrefix(string(content), header)
}

// Checksum returns the hexadecimal SHA-256 checksum of content.
func Checksum(content []byte) string {
	digest := sha256.Sum256(content)
	return hex.EncodeToString(digest[:])
}
