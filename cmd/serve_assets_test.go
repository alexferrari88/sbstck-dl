package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestServeHTMLIncludesObsidianFormat(t *testing.T) {
	assert.Contains(t, serveHTML, `value="obsidian-md"`)
}
