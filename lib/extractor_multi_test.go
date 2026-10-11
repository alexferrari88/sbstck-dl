package lib

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWriteToFileWithImagesNoImagesCompatibility(t *testing.T) {
	post := Post{Title: "Title", BodyHTML: "<p>Body</p>", CanonicalUrl: "https://example.com/p/post"}
	for _, format := range []string{"html", "md", "txt"} {
		t.Run(format, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "post."+format)
			result, err := post.WriteToFileWithImages(context.Background(), path, format, true, false, ImageQualityHigh, "images", false, nil, "files", nil)
			require.NoError(t, err)
			body, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, string(body), result.UpdatedHTML)
			require.NotNil(t, result.Images)
			require.Empty(t, result.Images)
		})
	}
}

func TestCanceledMediaNeverPublishesPost(t *testing.T) {
	post := Post{Title: "Title", BodyHTML: "<p>Body</p>"}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	path := filepath.Join(t.TempDir(), "post.html")
	require.NoError(t, os.WriteFile(path, []byte("prior valid post"), 0600))
	_, err := post.WriteToFileWithImages(ctx, path, "html", false, false, ImageQualityHigh, "images", false, nil, "files", nil)
	require.True(t, errors.Is(err, context.Canceled))
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "prior valid post", string(body))
}
