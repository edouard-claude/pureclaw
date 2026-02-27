package workspace

import (
	"testing"
)

func TestParseSections(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		wantKeys []string
		check    func(t *testing.T, sections map[string]string)
	}{
		{
			name:    "MultipleSections",
			content: "Preamble text\n\n## Name\n\nJohn\n\n## Role\n\nAssistant\n\n## Environment\n\nLinux",
			wantKeys: []string{"", "Name", "Role", "Environment"},
			check: func(t *testing.T, sections map[string]string) {
				if sections[""] != "Preamble text" {
					t.Errorf("preamble = %q", sections[""])
				}
				if sections["Name"] != "John" {
					t.Errorf("Name = %q", sections["Name"])
				}
				if sections["Role"] != "Assistant" {
					t.Errorf("Role = %q", sections["Role"])
				}
				if sections["Environment"] != "Linux" {
					t.Errorf("Environment = %q", sections["Environment"])
				}
			},
		},
		{
			name:     "NoSections",
			content:  "Just plain text\nwith multiple lines\nno headers",
			wantKeys: []string{""},
			check: func(t *testing.T, sections map[string]string) {
				if sections[""] != "Just plain text\nwith multiple lines\nno headers" {
					t.Errorf("content = %q", sections[""])
				}
			},
		},
		{
			name:     "EmptyString",
			content:  "",
			wantKeys: []string{""},
			check: func(t *testing.T, sections map[string]string) {
				if sections[""] != "" {
					t.Errorf("content = %q, want empty", sections[""])
				}
			},
		},
		{
			name:     "OnlyHeaders",
			content:  "## A\n## B",
			wantKeys: []string{"", "A", "B"},
			check: func(t *testing.T, sections map[string]string) {
				if sections[""] != "" {
					t.Errorf("preamble = %q, want empty", sections[""])
				}
				if sections["A"] != "" {
					t.Errorf("A = %q, want empty", sections["A"])
				}
				if sections["B"] != "" {
					t.Errorf("B = %q, want empty", sections["B"])
				}
			},
		},
		{
			name:     "HeaderLevels",
			content:  "# Title\n\n## Section\n\nContent with ### subsection\n\n### Not a split",
			wantKeys: []string{"", "Section"},
			check: func(t *testing.T, sections map[string]string) {
				if sections[""] != "# Title" {
					t.Errorf("preamble = %q", sections[""])
				}
				// ### should NOT cause a split, it stays in content
				sectionContent := sections["Section"]
				if sectionContent != "Content with ### subsection\n\n### Not a split" {
					t.Errorf("Section = %q", sectionContent)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sections := ParseSections(tt.content)

			if len(sections) != len(tt.wantKeys) {
				t.Errorf("got %d sections, want %d", len(sections), len(tt.wantKeys))
			}

			for _, key := range tt.wantKeys {
				if _, ok := sections[key]; !ok {
					t.Errorf("missing key %q", key)
				}
			}

			if tt.check != nil {
				tt.check(t, sections)
			}
		})
	}
}

func TestExtractSection(t *testing.T) {
	content := "# Title\n\n## Name\n\nJohn\n\n## Role\n\nAssistant"

	tests := []struct {
		name        string
		content     string
		sectionName string
		want        string
	}{
		{
			name:        "Found",
			content:     content,
			sectionName: "Name",
			want:        "John",
		},
		{
			name:        "NotFound",
			content:     content,
			sectionName: "Missing",
			want:        "",
		},
		{
			name:        "EmptyContent",
			content:     "",
			sectionName: "Name",
			want:        "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractSection(tt.content, tt.sectionName)
			if got != tt.want {
				t.Errorf("ExtractSection(%q) = %q, want %q", tt.sectionName, got, tt.want)
			}
		})
	}
}
