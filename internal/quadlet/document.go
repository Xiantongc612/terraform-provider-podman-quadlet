// Package quadlet renders and inspects provider-managed Podman Quadlet files.
package quadlet

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

const header = "# Managed by terraform-provider-podman-quadlet\n# Format: 1\n"

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

// Parse reads provider-managed Quadlet content while preserving repeated keys.
func Parse(content []byte) (map[string][]Pair, error) {
	if !Managed(content) {
		return nil, fmt.Errorf("quadlet file is not managed by this provider")
	}
	sections := make(map[string][]Pair)
	current := ""
	scanner := bufio.NewScanner(strings.NewReader(string(content)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			current = strings.TrimSuffix(strings.TrimPrefix(line, "["), "]")
			if current == "" {
				return nil, fmt.Errorf("empty Quadlet section")
			}
			continue
		}
		if current == "" {
			return nil, fmt.Errorf("entry outside a Quadlet section: %q", line)
		}
		key, value, found := strings.Cut(line, "=")
		if !found || key == "" {
			return nil, fmt.Errorf("invalid Quadlet entry: %q", line)
		}
		sections[current] = append(sections[current], Pair{Key: key, Value: value})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan Quadlet content: %w", err)
	}
	return sections, nil
}
