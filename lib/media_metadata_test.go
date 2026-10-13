package lib_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/alexferrari88/sbstck-dl/lib"
)

// Retain the public media metadata API while extending downloader metadata.
func TestMediaMetadataCompatibility(t *testing.T) {
	base := map[string]interface{}{
		"id": 123, "publication_id": 456, "type": "newsletter", "slug": "media-fixture",
		"post_date": "2026-10-13T00:00:00Z", "canonical_url": "https://example.invalid/p/media-fixture",
		"previous_post_slug": "before", "next_post_slug": "after", "cover_image": "https://example.invalid/cover.png",
		"description": "Legacy description", "subtitle": "Legacy subtitle", "wordcount": 42,
		"title": "Fixture title", "body_html": `<p>Legacy <strong>body</strong> &amp; text.</p><p>Second paragraph.</p>`,
	}
	baseJSON, _ := json.Marshal(base)
	var legacy lib.Post
	if err := json.Unmarshal(baseJSON, &legacy); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name       string
		input      map[string]interface{}
		serialized map[string]interface{}
	}{
		{"all_metadata", map[string]interface{}{
			"podcast_url":     "https://example.invalid/audio.mp3?download=1&source=fixture",
			"videoUpload":     map[string]interface{}{"id": "raw-video-id", "duration": 123.75, "url": "https://example.invalid/ignored-video-url", "extractedAudio": map[string]interface{}{"id": "raw-extracted-audio-id", "duration": 122.25}},
			"podcastUpload":   map[string]interface{}{"id": "raw-podcast-id", "duration": 61.5},
			"voiceoverUpload": map[string]interface{}{"id": "raw-voiceover-id", "duration": 62.125},
		}, map[string]interface{}{
			"podcast_url":     "https://example.invalid/audio.mp3?download=1&source=fixture",
			"videoUpload":     map[string]interface{}{"id": "raw-video-id", "duration": 123.75, "extractedAudio": map[string]interface{}{"id": "raw-extracted-audio-id", "duration": 122.25}},
			"podcastUpload":   map[string]interface{}{"id": "raw-podcast-id", "duration": 61.5},
			"voiceoverUpload": map[string]interface{}{"id": "raw-voiceover-id", "duration": 62.125},
		}},
		{"metadata_absent", nil, nil},
		{"metadata_null", map[string]interface{}{"podcast_url": nil, "videoUpload": nil, "podcastUpload": nil, "voiceoverUpload": nil}, nil},
		{"nested_audio_absent", map[string]interface{}{"videoUpload": map[string]interface{}{"id": "video-only", "duration": 3.25}}, map[string]interface{}{"videoUpload": map[string]interface{}{"id": "video-only", "duration": 3.25}}},
		{"nested_audio_null", map[string]interface{}{"videoUpload": map[string]interface{}{"id": "video-only", "duration": 3.25, "extractedAudio": nil}}, map[string]interface{}{"videoUpload": map[string]interface{}{"id": "video-only", "duration": 3.25}}},
		{"empty_upload_objects", map[string]interface{}{"podcast_url": "", "videoUpload": map[string]interface{}{"extractedAudio": map[string]interface{}{}}, "podcastUpload": map[string]interface{}{}, "voiceoverUpload": map[string]interface{}{}}, map[string]interface{}{"videoUpload": map[string]interface{}{"id": "", "duration": 0.0, "extractedAudio": map[string]interface{}{"id": "", "duration": 0.0}}, "podcastUpload": map[string]interface{}{"id": "", "duration": 0.0}, "voiceoverUpload": map[string]interface{}{"id": "", "duration": 0.0}}},
	}
	keys := []string{"podcast_url", "videoUpload", "podcastUpload", "voiceoverUpload"}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := map[string]interface{}{}
			for key, value := range base {
				input[key] = value
			}
			for key, value := range tc.input {
				input[key] = value
			}
			wrapped, err := json.Marshal(map[string]interface{}{"post": input})
			if err != nil {
				t.Fatal(err)
			}
			quoted, err := json.Marshal(string(wrapped))
			if err != nil {
				t.Fatal(err)
			}
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.URL.Path != "/p/fixture" {
					t.Errorf("unexpected media request: %s", r.URL.Path)
					http.NotFound(w, r)
					return
				}
				w.Header().Set("Content-Type", "text/html")
				fmt.Fprintf(w, "<html><head></head><body><script>window._preloads = JSON.parse(%s);</script></body></html>", quoted)
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			fetcher := lib.NewFetcher(lib.WithRatePerSecond(1000), lib.WithBurst(100), lib.WithTimeout(2*time.Second))
			post, err := lib.NewExtractor(fetcher).ExtractPost(ctx, server.URL+"/p/fixture")
			if err != nil {
				t.Fatal(err)
			}
			if requests != 1 {
				t.Fatalf("extraction made %d requests; expected one HTML fetch", requests)
			}
			if tc.name == "all_metadata" {
				if post.PodcastUrl != tc.input["podcast_url"] || post.VideoUpload == nil || post.VideoUpload.Id != "raw-video-id" || post.VideoUpload.Duration != 123.75 || post.VideoUpload.ExtractedAudio == nil || post.VideoUpload.ExtractedAudio.Id != "raw-extracted-audio-id" || post.VideoUpload.ExtractedAudio.Duration != 122.25 || post.PodcastUpload == nil || post.PodcastUpload.Id != "raw-podcast-id" || post.PodcastUpload.Duration != 61.5 || post.VoiceoverUpload == nil || post.VoiceoverUpload.Id != "raw-voiceover-id" || post.VoiceoverUpload.Duration != 62.125 {
					t.Fatalf("public fields did not retain all raw metadata: %+v", post)
				}
			}
			if tc.name == "metadata_absent" || tc.name == "metadata_null" {
				if post.PodcastUrl != "" || post.VideoUpload != nil || post.PodcastUpload != nil || post.VoiceoverUpload != nil {
					t.Fatalf("absent/null metadata did not retain zero values: %+v", post)
				}
			}
			if tc.name == "nested_audio_absent" || tc.name == "nested_audio_null" {
				if post.VideoUpload == nil || post.VideoUpload.ExtractedAudio != nil {
					t.Fatal("absent/null extractedAudio was not nil")
				}
			}
			out, err := post.ToJSON()
			if err != nil {
				t.Fatal(err)
			}
			var serialized map[string]interface{}
			if err := json.Unmarshal([]byte(out), &serialized); err != nil {
				t.Fatal(err)
			}
			media := map[string]interface{}{}
			for _, key := range keys {
				if value, exists := serialized[key]; exists {
					media[key] = value
					delete(serialized, key)
				}
			}
			wantMedia := tc.serialized
			if wantMedia == nil {
				wantMedia = map[string]interface{}{}
			}
			if !reflect.DeepEqual(media, wantMedia) {
				t.Fatalf("media JSON got %#v want %#v", media, wantMedia)
			}
			var wantLegacy map[string]interface{}
			if err := json.Unmarshal(baseJSON, &wantLegacy); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(serialized, wantLegacy) {
				t.Fatalf("legacy JSON changed: got %#v want %#v", serialized, wantLegacy)
			}
			if post.BodyHTML != legacy.BodyHTML || post.ToHTML(false) != legacy.ToHTML(false) || post.ToHTML(true) != legacy.ToHTML(true) || post.ToText(false) != legacy.ToText(false) || post.ToText(true) != legacy.ToText(true) {
				t.Fatal("legacy HTML/text output changed")
			}
			for _, withTitle := range []bool{false, true} {
				gotMD, err := post.ToMD(withTitle)
				if err != nil {
					t.Fatal(err)
				}
				wantMD, err := legacy.ToMD(withTitle)
				if err != nil {
					t.Fatal(err)
				}
				if gotMD != wantMD {
					t.Fatal("legacy Markdown output changed")
				}
			}
			var roundTrip lib.Post
			if err := json.Unmarshal([]byte(out), &roundTrip); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(post, roundTrip) {
				t.Fatal("Post ToJSON round trip changed fields")
			}
		})
	}
}
