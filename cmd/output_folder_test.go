package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResolveOutputFolder(t *testing.T) {
	tests := []struct {
		name         string
		outputFolder string
		downloadURL  string
		expected     string
	}{
		{
			name:         "uses substack subdomain when output is dot",
			outputFolder: ".",
			downloadURL:  "https://natesnewsletter.substack.com",
			expected:     "natesnewsletter",
		},
		{
			name:         "uses @handle path for substack.com",
			outputFolder: "",
			downloadURL:  "https://substack.com/@natesnewsletter",
			expected:     "natesnewsletter",
		},
		{
			name:         "keeps explicit output folder",
			outputFolder: "downloads",
			downloadURL:  "https://example.substack.com",
			expected:     "downloads",
		},
		{
			name:         "falls back to hostname",
			outputFolder: ".",
			downloadURL:  "https://example.com",
			expected:     "example.com",
		},
		{
			name:         "keeps dot when url invalid",
			outputFolder: ".",
			downloadURL:  "not a url",
			expected:     ".",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := resolveOutputFolder(tt.outputFolder, tt.downloadURL)
			assert.Equal(t, tt.expected, result)
		})
	}
}
