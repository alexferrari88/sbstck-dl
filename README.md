# Substack Downloader

Simple CLI tool to download one or all the posts from a Substack blog, and download Substack Notes for specific users.

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
  notes       Download Substack Notes for a specific user
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

### Downloading Substack Notes

You can download all Substack Notes for a specific user using their user ID. Notes are stored as comments in the user's activity feed, and this command fetches all activity and filters for notes vs regular comments.

```bash
Usage:
  sbstck-dl notes [flags]

Flags:
  -f, --format string          Output format (html, md, txt) (default "md")
  -h, --help                   help for notes
      --max-pages int          Maximum pages to fetch (default 10)
      --notes-only             Try to filter for notes vs regular comments
  -o, --output-dir string      Output directory (default "./notes")
      --user-id string         User ID (required, e.g., 303863305 for @nweiss)
      --username string        Username for organizing output (e.g., nweiss)

Global Flags:
      --after string    Download posts published after this date (format: YYYY-MM-DD)
      --before string   Download posts published before this date (format: YYYY-MM-DD)
      --cookie_name cookieName   Either substack.sid or connect.sid, based on your cookie (required for private newsletters)
      --cookie_val string        The substack.sid/connect.sid cookie value (required for private newsletters)
  -x, --proxy string    Specify the proxy url
  -r, --rate int        Specify the rate of requests per second (default 2)
  -v, --verbose         Enable verbose output
```

#### Finding User IDs

To find a user's ID, you can:
1. Visit the user's Substack profile page
2. Check the page source or network requests for API calls containing the user ID
3. The user ID is typically a numeric value (e.g., 309173054)

#### Notes Features

**Output Formats:**
- `html`: Full HTML format with styling and metadata
- `md`: Markdown format with headers and links (default)
- `txt`: Plain text format for maximum compatibility

**Filtering:**
- Use `--notes-only` to filter for actual notes vs regular post comments
- The tool uses the activity context type to distinguish between notes and comments

**Organization:**
- Notes are saved with timestamp-based filenames: `YYYYMMDD_HHMMSS_noteID.{format}`
- Output is organized by username or user ID in subdirectories
- Each note includes metadata like publication context, engagement stats, and original URLs

#### Examples

```bash
# Download notes for a specific user by user ID
sbstck-dl notes --user-id 303863305 --username nweiss

# Download notes in HTML format with verbose output
sbstck-dl notes --user-id 303863305 --format html --verbose

# Filter for actual notes only and fetch more pages
sbstck-dl notes --user-id 303863305 --notes-only --max-pages 20

# Download to custom directory
sbstck-dl notes --user-id 303863305 --output-dir ./my-notes --username nweiss

# Download in plain text format
sbstck-dl notes --user-id 303863305 --format txt --output-dir ./notes-txt
```

**Directory Structure for Notes:**
```
notes/
└── nweiss/              # Username or user_ID folder
    ├── 20240115_143000_12345.md
    ├── 20240114_120000_12346.md
    └── 20240113_094500_12347.md
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
- [x] Add support for downloading Substack Notes
- [x] Add tests
- [x] Add CI
- [x] Add documentation
- [x] Add support for private newsletters
- [x] Implement filtering by date
- [x] Implement resuming downloads
