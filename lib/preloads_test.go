package lib_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alexferrari88/sbstck-dl/lib"
	"github.com/stretchr/testify/require"
)

func TestExtractPostJSONScript(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<script id="preloads-json" type="application/json">{"post":{"id":31,"slug":"fixture","title":"Fixture","body_html":"<p>Readable body</p>"}}</script>
<script>window._preloads = JSON.parse(document.querySelector('script#preloads-json[type="application/json"]').textContent)</script>`)
	}))
	defer server.Close()

	post, err := lib.NewExtractor(nil).ExtractPost(context.Background(), server.URL+"/p/fixture")
	require.NoError(t, err)
	require.Equal(t, lib.Post{Id: 31, Slug: "fixture", Title: "Fixture", BodyHTML: "<p>Readable body</p>"}, post)
}

func TestExtractPostPreloadLayouts(t *testing.T) {
	// Literal JSON and expected fields independently pin the two escaping layers.
	payload := `{"post":{"id":31,"slug":"fixture","title":"Quoted \"title\" — café 🌱","body_html":"<p class=\"body\">C:\\notes\\new; literal \\n; &amp; 雪</p>\n<p>Second line</p>","podcast_url":"https://example.invalid/audio.mp3","videoUpload":{"id":"video","duration":1.5,"extractedAudio":{"id":"extracted","duration":1.25}},"podcastUpload":{"id":"podcast","duration":2.5},"voiceoverUpload":{"id":"voiceover","duration":3.5}}}`
	quoted, err := json.Marshal(payload)
	require.NoError(t, err)
	legacy := fmt.Sprintf(`window._preloads = JSON.parse(%s);`, quoted)
	analytics := `window.analytics = JSON.parse("{\"post\":{\"title\":\"Wrong analytics post\"}}");`
	jsonScript := `<script type="application/json" id="preloads-json">` + payload + `</script>`

	for _, tc := range []struct{ name, page string }{
		{"json-script", jsonScript},
		{"legacy", `<script>` + legacy + `</script>`},
		{"legacy-with-unrelated-script", `<script>/* window._preloads is loaded later */` + analytics + `</script><script>` + legacy + `</script>`},
		{"legacy-with-leading-analytics", `<script>` + analytics + legacy + `</script>`},
		{"legacy-with-trailing-analytics", `<script>` + legacy + analytics + `</script>`},
		{"json-script-with-legacy", `<script>window._preloads = JSON.parse("{\"post\":{\"title\":\"Old payload\"}}")</script>` + jsonScript},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				fmt.Fprint(w, `<meta property="og:image" content="https://example.invalid/cover.jpg"><div class="subtitle"> Fallback subtitle </div>`+tc.page)
			}))
			defer server.Close()

			post, err := lib.NewExtractor(nil).ExtractPost(context.Background(), server.URL+"/p/fixture")
			require.NoError(t, err)
			require.Equal(t, lib.Post{
				Id: 31, Slug: "fixture", Title: "Quoted \"title\" — café 🌱",
				BodyHTML: "<p class=\"body\">C:\\notes\\new; literal \\n; &amp; 雪</p>\n<p>Second line</p>",
				Subtitle: "Fallback subtitle", CoverImage: "https://example.invalid/cover.jpg",
				PodcastUrl: "https://example.invalid/audio.mp3",
				VideoUpload: &lib.VideoUpload{Id: "video", Duration: 1.5,
					ExtractedAudio: &lib.AudioUpload{Id: "extracted", Duration: 1.25}},
				PodcastUpload:   &lib.AudioUpload{Id: "podcast", Duration: 2.5},
				VoiceoverUpload: &lib.AudioUpload{Id: "voiceover", Duration: 3.5},
			}, post)
		})
	}
}

func TestExtractPostRejectsInvalidPreloads(t *testing.T) {
	for _, payload := range []string{
		``, `{broken`, `null`, `{}`, `{"post":null}`, `{"post":{}}`,
		`{"post":{"unknown":"metadata"}}`, `{"post":[]}`, `{"post":{"id":"invalid"}}`,
		`{"post":{"title":"Fixture"}} trailing`,
	} {
		for _, layout := range []string{"json-script", "legacy"} {
			t.Run(layout+"/"+payload, func(t *testing.T) {
				page := `<script id="preloads-json" type="application/json">` + payload + `</script>`
				if layout == "legacy" {
					quoted, err := json.Marshal(payload)
					require.NoError(t, err)
					page = fmt.Sprintf(`<script>window._preloads = JSON.parse(%s)</script>`, quoted)
				}
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					fmt.Fprint(w, page)
				}))
				defer server.Close()

				post, err := lib.NewExtractor(nil).ExtractPost(context.Background(), server.URL+"/p/fixture")
				require.ErrorContains(t, err, "failed to parse post data")
				require.Equal(t, lib.Post{}, post)
			})
		}
	}
}

func TestExtractPostRequiresPreloadSource(t *testing.T) {
	for _, page := range []string{
		`<p>No preload data</p>`,
		`<script type="application/json">{"post":{"title":"Unrelated JSON"}}</script>`,
		`<script>/* window._preloads is absent */ window.analytics = JSON.parse("{\"post\":{\"title\":\"Analytics\"}}");</script>`,
		`<script id="preloads-json" type="text/javascript">{"post":{"title":"Wrong script type"}}</script>`,
	} {
		t.Run(page, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, page)
			}))
			defer server.Close()

			post, err := lib.NewExtractor(nil).ExtractPost(context.Background(), server.URL+"/p/fixture")
			require.ErrorContains(t, err, "failed to extract post data")
			require.Equal(t, lib.Post{}, post)
		})
	}
}
