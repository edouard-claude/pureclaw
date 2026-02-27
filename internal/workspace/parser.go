package workspace

import "strings"

// ParseSections splits markdown content by "## " headers.
// Returns a map from section name (trimmed) to section content (trimmed).
// Content before the first ## header is stored under key "".
func ParseSections(content string) map[string]string {
	sections := make(map[string]string)
	currentKey := ""
	var currentContent strings.Builder

	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "## ") {
			sections[currentKey] = strings.TrimSpace(currentContent.String())
			currentKey = strings.TrimSpace(strings.TrimPrefix(line, "## "))
			currentContent.Reset()
		} else {
			currentContent.WriteString(line)
			currentContent.WriteString("\n")
		}
	}
	sections[currentKey] = strings.TrimSpace(currentContent.String())

	return sections
}

// ExtractSection returns the content of a specific ## section.
// Returns empty string if section not found.
func ExtractSection(content, sectionName string) string {
	sections := ParseSections(content)
	return sections[sectionName]
}
