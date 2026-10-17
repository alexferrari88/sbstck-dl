package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPreloadCLI(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "sbstck-dl")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	output, err := exec.Command("go", "build", "-o", binary, "..").CombinedOutput()
	require.NoError(t, err, string(output))

	for _, layout := range []string{"json-script", "legacy"} {
		for _, valid := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/valid=%t", layout, valid), func(t *testing.T) {
				payload := `{"post":{"id":31,"slug":"fixture","post_date":"2026-10-17T12:00:00Z","title":"Fixture","body_html":"<p>Readable <strong>body</strong> — café.</p>"}}`
				if !valid {
					payload = `{"post":null}`
				}
				page := `<script id="preloads-json" type="application/json">` + payload + `</script>`
				if layout == "legacy" {
					quoted, err := json.Marshal(payload)
					require.NoError(t, err)
					page = fmt.Sprintf(`<script>window._preloads = JSON.parse(%s)</script>`, quoted)
				}
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path != "/p/fixture" {
						http.NotFound(w, r)
						return
					}
					w.Header().Set("Content-Type", "text/html")
					fmt.Fprint(w, page)
				}))
				defer server.Close()
				dir := t.TempDir()
				output, err := exec.Command(binary, "download", "--url", server.URL+"/p/fixture",
					"--create-archive", "--format", "md", "--output", dir, "--rate", "1").CombinedOutput()
				if !valid {
					require.Error(t, err, string(output))
					require.Contains(t, string(output), "failed to parse post data: missing or empty post data")
					files, err := filepath.Glob(filepath.Join(dir, "*.md"))
					require.NoError(t, err)
					require.Empty(t, files, "failed extraction must not publish a post or index")
					return
				}
				require.NoError(t, err, string(output))
				post, err := os.ReadFile(filepath.Join(dir, "20261017_120000_fixture.md"))
				require.NoError(t, err)
				require.Equal(t, "# Fixture\n\nReadable **body** — café.", strings.TrimSpace(string(post)))
				index, err := os.ReadFile(filepath.Join(dir, "index.md"))
				require.NoError(t, err)
				require.Contains(t, string(index), "[Fixture](20261017_120000_fixture.md)")
				require.Contains(t, string(index), "October 17, 2026")
			})
		}
	}
}
