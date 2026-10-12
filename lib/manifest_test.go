package lib

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMediaManifest(t *testing.T) {
	t.Run("missing manifest loads empty", func(t *testing.T) {
		m, err := LoadMediaManifest(t.TempDir())
		require.NoError(t, err)
		assert.Empty(t, m.Posts)
		assert.False(t, m.IsComplete("post", MediaPodcast))
	})

	t.Run("round trip", func(t *testing.T) {
		dir := t.TempDir()
		audio := filepath.Join(dir, "podcasts", "ep.mp3")
		require.NoError(t, os.MkdirAll(filepath.Dir(audio), 0755))
		require.NoError(t, os.WriteFile(audio, []byte("audio"), 0644))

		m, err := LoadMediaManifest(dir)
		require.NoError(t, err)
		m.Record("ep", MediaPodcast, true, []string{audio})
		m.Record("ep", MediaVideos, false, nil)
		m.Record("text", MediaPodcast, true, nil)
		require.NoError(t, m.Save())

		loaded, err := LoadMediaManifest(dir)
		require.NoError(t, err)
		assert.Equal(t, []string{"podcasts/ep.mp3"}, (*loaded.Posts["ep"])[MediaPodcast].Files, "paths stored relative")
		assert.True(t, loaded.IsComplete("ep", MediaPodcast))
		assert.False(t, loaded.IsComplete("ep", MediaVideos))
		assert.True(t, loaded.IsComplete("text", MediaPodcast))
		assert.False(t, loaded.IsComplete("text", MediaVideos), "kind never recorded")
		assert.False(t, loaded.IsComplete("unknown", MediaPodcast))

		// A recorded file that was deleted makes the post incomplete again
		require.NoError(t, os.Remove(audio))
		assert.False(t, loaded.IsComplete("ep", MediaPodcast))
	})

	t.Run("corrupt manifest is an error", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, MediaManifestFile), []byte("{not json"), 0644))
		_, err := LoadMediaManifest(dir)
		assert.Error(t, err)
	})

	t.Run("save leaves no temporary file", func(t *testing.T) {
		dir := t.TempDir()
		m := NewMediaManifest(dir)
		m.Record("ep", MediaPodcast, true, nil)
		require.NoError(t, m.Save())

		entries, _ := os.ReadDir(dir)
		require.Len(t, entries, 1)
		assert.Equal(t, MediaManifestFile, entries[0].Name())
	})
}

func TestIsPaywalled(t *testing.T) {
	assert.False(t, (&Post{Audience: "everyone"}).IsPaywalled())
	assert.False(t, (&Post{}).IsPaywalled())
	assert.True(t, (&Post{Audience: "only_paid"}).IsPaywalled())
	assert.True(t, (&Post{Audience: "founding"}).IsPaywalled())
}
