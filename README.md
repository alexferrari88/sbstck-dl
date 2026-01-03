# Substack Downloader

Simple CLI tool to download one or all the posts from a Substack blog.

## Installation

### Downloading the binary

Check in the [releases](https://github.com/alexferrari88/sbstck-dl/releases) page for the latest version of the binary for your platform.
We provide binaries for Linux, MacOS and Windows.

### Using Go

```bash
go install github.com/alexferrari88/sbstck-dl
```

Your Go bin directory must be in your PATH. You can add it by adding the following line to your `.bashrc` or `.zshrc`:

```bash
export PATH=$PATH:$(go env GOPATH)/bin
```

## Usage

```bash
Usage:
  sbstck-dl [command]

Available Commands:
  archive     Regenerate the archive index from existing downloads
  download    Download individual posts or the entire public archive
  help        Help about any command
  list        List the posts of a Substack
  serve       Launch a local-only web UI
  version     Print the version number of sbstck-dl

Flags:
      --after string             Download posts published after this date (format: YYYY-MM-DD)
      --before string            Download posts published before this date (format: YYYY-MM-DD)
      --cookie_name cookieName   Either substack.sid or connect.sid, based on your cookie (required for private newsletters)
      --cookie_val string        The substack.sid/connect.sid cookie value (required for private newsletters; or set SBSTCK_COOKIE_VAL)
      --cookie-val-file string   Read cookie value from a file (overrides SBSTCK_COOKIE_VAL)
      --cookie-jar string        Read cookies from a Netscape cookie jar file (cookies.txt)
      --cookie-keychain string   Read cookie value from OS keychain/credential manager
      --notion-labels string     Path to a YAML/JSON map of Notion URL to label for notion-links output
  -h, --help                     help for sbstck-dl
      --concurrency int          Alias for --max-workers
      --log-format string        Log format (text or json) (default "text")
      --max-workers int          Maximum parallel workers for downloading posts (rate limiting still applies) (default 10)
  -x, --proxy string             Specify the proxy url
  -r, --rate int                 Specify the rate of requests per second (default 2)
  -v, --verbose                  Enable verbose output

Use "sbstck-dl [command] --help" for more information about a command.
```

### Downloading posts

You can provide the url of a single post or the main url of the Substack you want to download.

By providing the main URL of a Substack, the downloader will download all the posts of the archive.

When downloading the full archive, if the downloader is interrupted, at the next execution it will resume the download of the remaining posts.
A `manifest.json` file is written in the output directory with canonical URLs, local paths, download times, and content hashes.
On reruns, URLs already recorded in the manifest (with matching format and existing file) are skipped.
When using `html+md` (HTML + Obsidian Markdown), each format is checked independently so missing outputs are downloaded.
If any posts fail to download, a `failed-urls.txt` file is written in the output directory for retrying later.
If `--output` is omitted or set to `.`, files are written under a folder named after the publication.
Use `--refresh-updated` to re-download posts when the sitemap `lastmod` is newer than the manifest.
Use `--layout` to control how output files are organized (flat, year/month, year/slug).
Use `--write-metadata` to write a JSON sidecar for each downloaded post (includes `notion_links` when present).

```bash
Usage:
  sbstck-dl download [flags]

Flags:
      --add-source-url         Add the original post URL at the end of the downloaded file
      --create-archive         Create an archive index page linking all downloaded posts
      --download-files         Download file attachments locally and update content to reference local files
      --download-images        Download images locally and update content to reference local files
      --fail-fast             Stop the download on the first error
  -d, --dry-run                Enable dry run
      --file-extensions string Comma-separated list of file extensions to download (e.g., 'pdf,docx,txt'). If empty, downloads all file types
      --files-dir string       Directory name for downloaded file attachments (default "files")
      --force                 Redownload posts even if they already exist
  -f, --format string          Specify the output format (options: "html", "md", "obsidian-md", "txt", or "html+md") (default "html+md")
  -h, --help                   help for download
      --image-quality string   Image quality to download (options: "high", "medium", "low") (default "high")
      --images-dir string      Directory name for downloaded images (default "images")
      --layout string          Output layout (flat, year/month, year/slug) (default "flat")
  -o, --output string          Specify the download directory (default: publication name when empty or ".")
      --write-metadata         Write a JSON sidecar with post metadata
      --refresh-updated        Refresh posts when sitemap lastmod is newer than the manifest
      --skip-existing          Skip existing posts (default for archive downloads)
      --continue-on-error      Continue downloading after errors (default)
  -u, --url string             Specify the Substack url

Global Flags:
      --after string    Download posts published after this date (format: YYYY-MM-DD)
      --before string   Download posts published before this date (format: YYYY-MM-DD)
      --config string   Load options from a YAML/JSON config file
      --cookie_name cookieName   Either substack.sid or connect.sid, based on your cookie (required for private newsletters)
      --cookie_val string        The substack.sid/connect.sid cookie value (required for private newsletters; or set SBSTCK_COOKIE_VAL)
      --cookie-val-file string   Read cookie value from a file (overrides SBSTCK_COOKIE_VAL)
      --cookie-jar string        Read cookies from a Netscape cookie jar file (cookies.txt)
      --cookie-keychain string   Read cookie value from OS keychain/credential manager
      --notion-labels string     Path to a YAML/JSON map of Notion URL to label for notion-links output
      --concurrency int  Alias for --max-workers
      --log-format string  Log format (text or json) (default "text")
      --max-workers int  Maximum parallel workers for downloading posts (rate limiting still applies) (default 10)
  -x, --proxy string    Specify the proxy url
  -r, --rate int        Specify the rate of requests per second (default 2)
  -v, --verbose         Enable verbose output
```

#### Output formats

- `html`: Preserves Substack formatting best.
- `html+md`: Download HTML plus Obsidian Markdown in one run (each format skips existing files separately). This is the default.
- `md`: Standard Markdown (no Obsidian-specific transformations).
- `obsidian-md`: Markdown optimized for Obsidian (wikilinks/embeds, LF line endings).
- `txt`: Plain text.

Example (download HTML + Obsidian Markdown, only missing outputs):
```bash
sbstck-dl download --url https://example.substack.com --format html+md
```

For `obsidian-md`, YAML frontmatter is derived only from Substack metadata (title, tags, and the earliest publication date). The current/system date is never used.

Example (obsidian-md conversion):

Before (md):
```md
[Label](OtherNote.md)
![alt](./assets/img.png)
```

After (obsidian-md):
```md
[[OtherNote|Label]]
![[assets/img.png]]
```

#### Adding Source URL

If you use the `--add-source-url` flag, each downloaded file will have the following line appended to its content:

`original content: POST_URL`

Where `POST_URL` is the canonical URL of the downloaded post. For HTML format, this will be wrapped in a small paragraph with a link.

#### Downloading Images

Use the `--download-images` flag to download all images from Substack posts locally. This ensures posts remain accessible even if images are deleted from Substack's CDN.

**Features:**
- Downloads images at optimal quality (high/medium/low)
- Creates organized directory structure: `{output}/images/{post-slug}/`
- Updates HTML/Markdown content to reference local image paths
- Handles all Substack image formats and CDN patterns
- Graceful error handling for individual image failures

**Examples:**

```bash
# Download posts with high-quality images (default)
sbstck-dl download --url https://example.substack.com --download-images

# Download with medium quality images
sbstck-dl download --url https://example.substack.com --download-images --image-quality medium

# Download with custom images directory name
sbstck-dl download --url https://example.substack.com --download-images --images-dir assets

# Download single post with images in markdown format
sbstck-dl download --url https://example.substack.com/p/post-title --download-images --format md
```

**Image Quality Options:**
- `high`: 1456px width (best quality, larger files)
- `medium`: 848px width (balanced quality/size)
- `low`: 424px width (smaller files, mobile-optimized)

**Directory Structure:**
```
output/
├── 20231201_120000_post-title.html
└── images/
    └── post-title/
        ├── image1_1456x819.jpeg
        ├── image2_848x636.png
        └── image3_1272x720.webp
```

#### Downloading File Attachments

Use the `--download-files` flag to download all file attachments from Substack posts locally. This ensures posts remain accessible even if files are removed from Substack's servers.

**Features:**
- Downloads file attachments using CSS selector `.file-embed-button.wide`
- Optional file extension filtering (e.g., only PDFs and Word documents)
- Creates organized directory structure: `{output}/files/{post-slug}/`
- Updates HTML content to reference local file paths
- Handles filename sanitization and collision avoidance
- Graceful error handling for individual file download failures

**Examples:**

```bash
# Download posts with all file attachments
sbstck-dl download --url https://example.substack.com --download-files

# Download only specific file types
sbstck-dl download --url https://example.substack.com --download-files --file-extensions "pdf,docx,txt"

# Download with custom files directory name
sbstck-dl download --url https://example.substack.com --download-files --files-dir attachments

# Download single post with both images and file attachments
sbstck-dl download --url https://example.substack.com/p/post-title --download-images --download-files --format md
```

**File Extension Filtering:**
- Specify extensions without dots: `pdf,docx,txt`
- Case insensitive matching
- If no extensions specified, downloads all file types

**Directory Structure with Files:**
```
output/
├── 20231201_120000_post-title.html
├── images/
│   └── post-title/
│       ├── image1_1456x819.jpeg
│       └── image2_848x636.png
└── files/
    └── post-title/
        ├── document.pdf
        ├── spreadsheet.xlsx
        └── presentation.pptx
```

#### Creating Archive Index Pages

Use the `--create-archive` flag to generate an organized index page that links all downloaded posts with their metadata. This creates a beautiful overview of your downloaded content, making it easy to browse and access your Substack archive.

**Features:**
- Creates `index.{format}` file matching your selected output format (HTML/Markdown/Obsidian Markdown/Text)
- Links to all downloaded posts using relative file paths
- Displays post titles, publication dates, and download timestamps
- Shows post descriptions/subtitles and cover images when available
- Automatically sorts posts by publication date (newest first)
- HTML archive includes search, sorting (newest/oldest/title), and date range filters
- Extracts Notion links into `notion-links.html` and `notion-links.md` (deduped and grouped by post)
- Shows Notion link counts per post with a badge linking to the Notion links list
- Adds a domain index (Notion, GitHub, Docs, etc.) for one-click filtering of posts with outbound links
- Supports optional Notion label maps to show friendly names for links
- Works with both single post and bulk downloads

**Examples:**

```bash
# Download entire archive and create index page
sbstck-dl download --url https://example.substack.com --create-archive

# Create archive index in Markdown format
sbstck-dl download --url https://example.substack.com --create-archive --format md

# Build archive over time with single posts
sbstck-dl download --url https://example.substack.com/p/post-title --create-archive

# Complete download with all features
sbstck-dl download --url https://example.substack.com --download-images --download-files --create-archive

# Custom directory structure with archive
sbstck-dl download --url https://example.substack.com --create-archive --images-dir assets --files-dir attachments
```

#### Notion label map

Use `--notion-labels` to provide a JSON or YAML map of Notion URLs to friendly labels for `notion-links.html` and `notion-links.md`.
URLs are normalized (tracking params removed) before matching.

JSON:
```json
{
  "https://www.notion.so/Workspace/Page-123?utm_source=x": "Project Plan",
  "https://example.notion.site/Team-Wiki": "Team Wiki"
}
```

YAML:
```yaml
https://www.notion.so/Workspace/Page-123?utm_source=x: Project Plan
https://example.notion.site/Team-Wiki: Team Wiki
```

**Archive Content Per Post:**
- **Title**: Clickable link to the downloaded post file
- **Publication Date**: When the post was originally published on Substack
- **Download Date**: When you downloaded the post locally  
- **Description**: Post subtitle or description (when available)
- **Cover Image**: Featured image from the post (when available)

**Archive Format Examples:**

*HTML Format:* Styled webpage with images, organized post cards, and hover effects
*Markdown Format:* Clean markdown with headers, links, and image references
*Text Format:* Plain text listing with all metadata for maximum compatibility

**Directory Structure with Archive:**
```
output/
├── index.html                     # Archive index page
├── 20231201_120000_post-title.html
├── 20231115_090000_another-post.html
├── images/
│   ├── post-title/
│   │   └── image1_1456x819.jpeg
│   └── another-post/
│       └── image2_848x636.png
└── files/
    ├── post-title/
    │   └── document.pdf
    └── another-post/
        └── spreadsheet.xlsx
```

### Listing posts

Use `--json` for structured output. Add `--with-metadata` to fetch title/date for each post (rate-limited).

```bash
Usage:
  sbstck-dl list [flags]

Flags:
  -h, --help            help for list
      --json            Output JSON
  -u, --url string      Specify the Substack url
      --with-metadata   Include title and date in JSON output (fetches each post)

Global Flags:
      --after string    Download posts published after this date (format: YYYY-MM-DD)
      --before string   Download posts published before this date (format: YYYY-MM-DD)
      --config string   Load options from a YAML/JSON config file
      --cookie_name cookieName   Either substack.sid or connect.sid, based on your cookie (required for private newsletters)
      --cookie_val string        The substack.sid/connect.sid cookie value (required for private newsletters; or set SBSTCK_COOKIE_VAL)
      --cookie-val-file string   Read cookie value from a file (overrides SBSTCK_COOKIE_VAL)
      --cookie-jar string        Read cookies from a Netscape cookie jar file (cookies.txt)
      --cookie-keychain string   Read cookie value from OS keychain/credential manager
      --notion-labels string     Path to a YAML/JSON map of Notion URL to label for notion-links output
      --concurrency int  Alias for --max-workers
      --log-format string  Log format (text or json) (default "text")
      --max-workers int  Maximum parallel workers for downloading posts (rate limiting still applies) (default 10)
  -x, --proxy string    Specify the proxy url
  -r, --rate int        Specify the rate of requests per second (default 2)
  -v, --verbose         Enable verbose output
```

### Private Newsletters

In order to download the full text of private newsletters you need to provide the cookie name and value of your session.
The cookie name is either `substack.sid` or `connect.sid`, based on your cookie.
To get the cookie value you can use the developer tools of your browser.
Once you have the cookie name and value, you can pass them to the downloader using the `--cookie_name` and `--cookie_val` flags.
To avoid storing secrets in shell history, you can also set `SBSTCK_COOKIE_VAL` and omit `--cookie_val`.
You can also read the cookie value from a file using `--cookie-val-file`.
You can also provide a Netscape cookie jar (`cookies.txt`) using `--cookie-jar`.
You can also read the cookie value from your OS keychain using `--cookie-keychain`.

Steps to get the cookie value:
1) Log in to the private Substack in your browser.
2) Open developer tools and go to the Application/Storage tab.
3) Select Cookies for the Substack domain and copy the value for `substack.sid` or `connect.sid`.
4) Paste the cookie value into `--cookie_val`, or store it in a file/keychain and reference it with `--cookie-val-file` or `--cookie-keychain`.

Security notes:
- Treat the cookie like a password and keep it out of chat logs, screenshots, and shared terminals.
- Prefer `SBSTCK_COOKIE_VAL`, `--cookie-val-file`, or `--cookie-keychain` to avoid shell history.
- Rotate the cookie by logging out and back in if you suspect it was exposed.

#### Example

```bash
sbstck-dl download --url https://example.substack.com --cookie_name substack.sid --cookie_val COOKIE_VALUE
```

### Recommended recipes

Full archive with assets + indexes:

```bash
sbstck-dl download --url https://example.substack.com --output archive --format html --download-images --download-files --create-archive --layout year/month --write-metadata
```

Incremental updates (skip existing, refresh newer posts):

```bash
sbstck-dl download --url https://example.substack.com --output archive --format html --skip-existing --refresh-updated --create-archive
```

Private newsletter via keychain + incremental sync:

```bash
sbstck-dl download --url https://example.substack.com --output archive --format html --cookie_name substack.sid --cookie-keychain my-substack-cookie --skip-existing
```

### Regenerating the archive index

```bash
Usage:
  sbstck-dl archive [flags]

Flags:
  -f, --format string   Specify the output format (options: "html", "md", "obsidian-md", "txt", or "html+md") (default "html+md")
  -h, --help            help for archive
  -o, --output string   Specify the download directory (default ".")

Global Flags:
      --after string    Download posts published after this date (format: YYYY-MM-DD)
      --before string   Download posts published before this date (format: YYYY-MM-DD)
      --config string   Load options from a YAML/JSON config file
      --cookie_name cookieName   Either substack.sid or connect.sid, based on your cookie (required for private newsletters)
      --cookie_val string        The substack.sid/connect.sid cookie value (required for private newsletters; or set SBSTCK_COOKIE_VAL)
      --cookie-val-file string   Read cookie value from a file (overrides SBSTCK_COOKIE_VAL)
      --cookie-jar string        Read cookies from a Netscape cookie jar file (cookies.txt)
      --cookie-keychain string   Read cookie value from OS keychain/credential manager
      --notion-labels string     Path to a YAML/JSON map of Notion URL to label for notion-links output
      --concurrency int  Alias for --max-workers
      --log-format string  Log format (text or json) (default "text")
      --max-workers int  Maximum parallel workers for downloading posts (rate limiting still applies) (default 10)
  -x, --proxy string    Specify the proxy url
  -r, --rate int        Specify the rate of requests per second (default 2)
  -v, --verbose         Enable verbose output
```

### Serving the UI

The UI server binds to `127.0.0.1` only. Use `--open` to auto-open your browser, or copy the printed URL.
The UI assets are embedded in the binary, so the serve command works without external files.
It includes a step-by-step wizard with Basic and Advanced presets that map directly to CLI flags, plus inline tips and recommended defaults.
Inputs are validated for URL, proxy, and date formats with actionable error messages.
Use the Test connection step to fetch `sitemap.xml` and verify a private post URL with your cookie.
Use the Dry-run preview step to see counts, date range, and download/skip totals before running.
Use the Incremental sync view to check your last run, see new posts, and rerun quickly.
Use Profiles and config to save presets locally or export/import a JSON config file for the CLI.
Profiles avoid storing cookie values by default. You can optionally save cookie values to the OS keychain and reference them by name in the UI.
From the Review step you can run a background download job with live progress, logs, retries, and cancel controls.
The UI backend exposes local-only `/api/...` endpoints and protects POSTs with a CSRF token.

```bash
Usage:
  sbstck-dl serve [flags]

Flags:
      --open       Open the UI in your default browser
      --port int   Port to bind the local UI server (default 8787)

Global Flags:
      --after string    Download posts published after this date (format: YYYY-MM-DD)
      --before string   Download posts published before this date (format: YYYY-MM-DD)
      --config string   Load options from a YAML/JSON config file
      --cookie_name cookieName   Either substack.sid or connect.sid, based on your cookie (required for private newsletters)
      --cookie_val string        The substack.sid/connect.sid cookie value (required for private newsletters; or set SBSTCK_COOKIE_VAL)
      --cookie-val-file string   Read cookie value from a file (overrides SBSTCK_COOKIE_VAL)
      --cookie-jar string        Read cookies from a Netscape cookie jar file (cookies.txt)
      --cookie-keychain string   Read cookie value from OS keychain/credential manager
      --notion-labels string     Path to a YAML/JSON map of Notion URL to label for notion-links output
      --concurrency int  Alias for --max-workers
      --log-format string  Log format (text or json) (default "text")
      --max-workers int  Maximum parallel workers for downloading posts (rate limiting still applies) (default 10)
  -x, --proxy string    Specify the proxy url
  -r, --rate int        Specify the rate of requests per second (default 2)
  -v, --verbose         Enable verbose output
```

```bash
SBSTCK_COOKIE_VAL=COOKIE_VALUE sbstck-dl download --url https://example.substack.com --cookie_name substack.sid
```

```bash
sbstck-dl download --url https://example.substack.com --cookie_name substack.sid --cookie-val-file path/to/cookie.txt
```

```bash
sbstck-dl download --url https://example.substack.com --cookie-jar path/to/cookies.txt
```

## Thanks

- [wemoveon2](https://github.com/wemoveon2) and [lenzj](https://github.com/lenzj) for the discussion and help implementing the support for private newsletters

## TODO

All roadmap items are complete.

## Completed

#### P0 — Incremental reruns & correctness
- [x] (P0) Add a download manifest (canonical URL -> local path, timestamps, hashes) for reliable incremental sync
- [x] (P0) Optimize reruns: reliably skip already-downloaded posts and only fetch new ones (beyond slug matching)
- [x] (P0) Fix `--before/--after` date filtering to compare parsed dates (not string comparison)

#### P1 — Performance & robustness
- [x] (P1) Speed up incremental reruns: avoid per-URL `Glob` scanning; pre-index existing downloads once
- [x] (P1) Add `--concurrency/--max-workers` to control parallelism (and document how it interacts with `--rate`)
- [x] (P1) Add structured logs and a summary report (downloaded/skipped/failed)
- [x] (P1) Write a "failed URLs" file for retrying later
- [x] (P1) Add `--fail-fast` / `--continue-on-error` behavior switches

#### P1 — Incremental controls & updates
- [x] (P1) Add `--force` / `--skip-existing` switches (redownload/overwrite vs incremental)
- [x] (P1) Detect updated posts (e.g. compare sitemap `lastmod` or content hash) and optionally refresh local copies
- [x] (P1) Improve resume behavior across format/layout changes (based on manifest, not only slug matching)

#### P1 — Private newsletters (auth + safety)
- [x] (P1) Support reading cookie value from an env var (avoid shell history)
- [x] (P1) Support reading cookie value from a file (e.g. `--cookie-val-file`)
- [x] (P1) Support importing cookies from a Netscape cookie jar (optional)

#### P2 - Output & archive UX
- [x] (P2) Add `--layout` for output structure (e.g. `flat` (current), `year/month`, `year/slug`)
- [x] (P2) Optionally write per-post metadata sidecar (e.g. `post.json`) or Markdown front matter
- [x] (P2) Add an `archive` command to regenerate `index.{format}` from existing downloads (no re-download)
- [x] (P2) Add optional filtering/sorting options for the archive page (e.g. by date range, newest/oldest)
- [x] (P2) Extract and index Notion links across posts into a separate `notion-links.{html,md}` (deduped + grouped by post)
- [x] (P2) Add a Notion badge/count in `index.html` for posts containing Notion links, with quick navigation to the links list
- [x] (P2) Persist extracted links in per-post metadata (sidecar JSON/front matter) to avoid re-parsing content on reruns
- [x] (P2) Add an index-by-domain view (Notion, Google Docs, GitHub, etc.) so Notion links become a one-click filter
- [x] (P2) Normalize/dedupe extracted Notion URLs (strip tracking params, canonicalize hosts/paths)
- [x] (P2) Support a user-provided label map (YAML/JSON) to display friendly names for frequently referenced Notion pages

#### P2 - UI (guided local web app)
- [x] (P2) Add a `serve` (or `ui`) command that launches a local-only web UI (bind `127.0.0.1`) for guided runs
- [x] (P2) Build a step-by-step “wizard” flow with presets (Basic vs Advanced) that maps 1:1 to CLI flags
- [x] (P2) Add inline help/tooltips and recommended defaults (format, images/files, rate limits) for non-technical users
- [x] (P2) Validate inputs in the UI (URL/proxy/date formats) and provide actionable error messages
- [x] (P2) Add a “Test connection” step (fetch `sitemap.xml`, verify cookie works for a known private post)
- [x] (P2) Add a dry-run preview screen: posts count, newest/oldest dates, and what will be downloaded/skipped
- [x] (P2) Run downloads as background jobs with live progress (per post), logs, retry stats, and a Cancel button
- [x] (P2) Add a “Rerun later”/incremental sync view (show last run, new posts since last run, run again)
- [x] (P2) Add profile management: save/load run presets and export/import a config file from the UI
- [x] (P2) Improve secret handling in UI: don’t persist cookies by default; optional secure storage (OS keychain/credential manager)
- [x] (P2) Add `--open` to auto-open the browser when starting the UI, and print the URL for manual open
- [x] (P2) Bundle UI assets into the binary (Go `embed`) so the UI ships as a single executable
- [x] (P2) Expose a small local API (`/api/...`) for list/download/status, with basic hardening (CSRF token, no remote bind)
- [x] (P2) Refactor core download logic into a reusable “runner” API so both CLI and UI share the same codepath
- [x] (P2) Add API-level tests (`httptest`) for the UI backend and keep `go test ./...` green

#### P2 - Config, CLI & docs
- [x] (P2) Load options from a config file (e.g. `--config config.yaml`) and merge with CLI flags
- [x] (P2) Expand the "Private Newsletters" docs with a step-by-step cookie retrieval + security notes
- [x] (P2) Add a "recommended recipes" section (full archive + assets + index + incremental updates)
- [x] (P2) Add `list --json` output; optionally add a metadata mode that also fetches title/date (rate-limited)
