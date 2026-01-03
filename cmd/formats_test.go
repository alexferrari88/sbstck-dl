package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseFormats(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []string
		wantErr bool
	}{
		{
			name:  "empty defaults to html and obsidian",
			input: "",
			want:  []string{"html", "obsidian-md"},
		},
		{
			name:  "single format normalized",
			input: "HTML",
			want:  []string{"html"},
		},
		{
			name:  "plus-separated formats prefer obsidian with html",
			input: "html+md",
			want:  []string{"html", "obsidian-md"},
		},
		{
			name:  "comma-separated formats prefer obsidian with html",
			input: "html,md",
			want:  []string{"html", "obsidian-md"},
		},
		{
			name:  "deduplicates formats",
			input: "html+md+html",
			want:  []string{"html", "obsidian-md"},
		},
		{
			name:  "md alone stays markdown",
			input: "md",
			want:  []string{"md"},
		},
		{
			name:    "invalid format",
			input:   "html+doc",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseFormats(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseSingleFormat(t *testing.T) {
	format, err := parseSingleFormat("md")
	require.NoError(t, err)
	assert.Equal(t, "md", format)

	_, err = parseSingleFormat("html+md")
	require.Error(t, err)
}
