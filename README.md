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
  download    Download individual posts or the entire public archive
  help        Help about any command
  list        List the posts of a Substack
  version     Print the version number of sbstck-dl

Flags:
      --after string             Download posts published after this date (format: YYYY-MM-DD)
      --before string            Download posts published before this date (format: YYYY-MM-DD)
      --cookie_name cookieName   Either substack.sid or connect.sid, based on your cookie (required for private newsletters)
      --cookie_val string        The substack.sid/connect.sid cookie value (required for private newsletters)
  -h, --help                     help for sbstck-dl
  -x, --proxy string             Specify the proxy url
  -r, --rate int                 Specify the rate of requests per second (default 2)
  -v, --verbose                  Enable verbose output

Use "sbstck-dl [command] --help" for more information about a command.
```

### Downloading posts

You can provide the url of a single post or the main url of the Substack you want to download.

By providing the main URL of a Substack, the downloader will download all the posts of the archive.

Publication discovery first reads `/sitemap.xml`. If the sitemap is missing or unusable, it pages through the publication's public `/api/v1/archive` endpoint until an empty page. This also works with custom domains. Feeds and the initial `/archive` HTML page contain recent posts, so they are not used as complete archives. A discovery error or cancellation stops the operation rather than returning a partial archive.

Date filters use sitemap `lastmod` when a valid sitemap exists and publication `post_date` for the API fallback. Existing date comparisons are unchanged; edited posts and date-only versus timestamp boundaries can produce different results between these sources. The public archive API can change, and posts added or removed during pagination can shift results; discovery is not an atomic snapshot.

When downloading the full archive, if the downloader is interrupted, at the next execution it will resume the download of the remaining posts.

```bash
Usage:
  sbstck-dl download [flags]

Flags:
      --add-source-url         Add the original post URL at the end of the downloaded file
      --create-archive         Create an archive index page linking all downloaded posts
      --download-files         Download file attachments locally and update content to reference local files
      --download-images        Download images locally and update content to reference local files
  -d, --dry-run                Enable dry run
      --file-extensions string Comma-separated list of file extensions to download (e.g., 'pdf,docx,txt'). If empty, downloads all file types
      --files-dir string       Directory name for downloaded file attachments (default "files")
  -f, --format string          Specify the output format (options: "html", "md", "txt" (default "html")
  -h, --help                   help for download
      --image-quality string   Image quality to download (options: "high", "medium", "low") (default "high")
      --images-dir string      Directory name for downloaded images (default "images")
  -o, --output string          Specify the download directory (default ".")
  -u, --url string             Specify the Substack url

Global Flags:
      --after string    Download posts published after this date (format: YYYY-MM-DD)
      --before string   Download posts published before this date (format: YYYY-MM-DD)
      --cookie_name cookieName   Either substack.sid or connect.sid, based on your cookie (required for private newsletters)
      --cookie_val string        The substack.sid/connect.sid cookie value (required for private newsletters)
  -x, --proxy string    Specify the proxy url
  -r, --rate int        Specify the rate of requests per second (default 2)
  -v, --verbose         Enable verbose output
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
- Creates `index.{format}` file matching your selected output format (HTML/Markdown/Text)
- Links to all downloaded posts using relative file paths
- Displays post titles, publication dates, and download timestamps
- Shows post descriptions/subtitles and cover images when available
- Automatically sorts posts by publication date (newest first)
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

Each successfully downloaded local post file is indexed once; distinct saved files remain distinct entries. Archive indexes are cumulative: incremental bulk downloads and single-post downloads retain earlier local posts. Re-running with no new posts also rebuilds the selected index without fetching existing posts. HTML, Markdown, and text have separate indexes and metadata files (`.sbstck-dl-archive-html.json`, `.sbstck-dl-archive-md.json`, and `.sbstck-dl-archive-txt.json`). Keep these files with the posts to retain subtitles, descriptions, cover URLs, and download times; they contain index metadata, not post bodies.

Older output folders are recovered from the downloader's root-level `YYYYMMDD_HHMMSS_slug.format` files (and `_slug.format` files for posts without valid dates). Titles come from the saved post heading; if unavailable, the filename slug is used. Publication dates come from the filename, which does not retain the original timezone; recovered timestamps use UTC. The file modification time is the fallback download time. Legacy subtitles, descriptions, and cover URLs are unavailable when they were not saved as metadata. Empty files, directories, and symlinks are excluded.

Posts, archive indexes, and metadata files are published with temporary files and atomic replacement. Restoration errors stop the command before downloads and preserve the existing index. Publication and post-write errors return a nonzero exit status. Metadata is saved before the index; if index publication fails, the prior index stays intact and a later run can use the saved metadata to rebuild it. Dry runs do not write either file. Only successfully written posts are added to the current index; a failed overwrite retains the prior complete post and its index entry.

With `--create-archive --download-images`, HTML and Markdown archive covers use local assets. A matching successful body-image download is reused; otherwise the cover is downloaded into a source-specific `covers` subdirectory under the configured `--images-dir`. This prevents a cover from replacing an unrelated body image with the same filename. Nested image directories are supported, and index links stay relative to the archive folder.

Cover localization also runs when every post is already downloaded. Metadata keeps the original cover URL and the successful local path, so later runs reuse existing assets, retry missing covers, and continue working after the archive folder is moved. Without `--download-images`, no cover requests are made. A failed cover download emits a warning and keeps the remote URL as a fallback; the post and index remain usable. Cancellation stops index publication with an error. Legacy files without retained cover-source metadata cannot recover covers without downloading the post again.

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

```bash
Usage:
  sbstck-dl list [flags]

Flags:
  -h, --help         help for list
  -u, --url string   Specify the Substack url

Global Flags:
      --after string    Download posts published after this date (format: YYYY-MM-DD)
      --before string   Download posts published before this date (format: YYYY-MM-DD)
      --cookie_name cookieName   Either substack.sid or connect.sid, based on your cookie (required for private newsletters)
      --cookie_val string        The substack.sid/connect.sid cookie value (required for private newsletters)
  -x, --proxy string    Specify the proxy url
  -r, --rate int        Specify the rate of requests per second (default 2)
  -v, --verbose         Enable verbose output
```

### Private Newsletters

In order to download the full text of private newsletters you need to provide the cookie name and value of your session.
The cookie name is either `substack.sid` or `connect.sid`, based on your cookie.
To get the cookie value you can use the developer tools of your browser.
Once you have the cookie name and value, you can pass them to the downloader using the `--cookie_name` and `--cookie_val` flags.

#### Example

```bash
sbstck-dl download --url https://example.substack.com --cookie_name substack.sid --cookie_val COOKIE_VALUE
```

## Thanks

- [wemoveon2](https://github.com/wemoveon2) and [lenzj](https://github.com/lenzj) for the discussion and help implementing the support for private newsletters

## TODO

- [x] Improve retry logic
- [ ] Implement loading from config file
- [x] Add support for downloading images
- [x] Add support for downloading file attachments
- [x] Add archive index page functionality
- [x] Add tests
- [x] Add CI
- [x] Add documentation
- [x] Add support for private newsletters
- [x] Implement filtering by date
- [x] Implement resuming downloads
