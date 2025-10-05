# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview
This is a Go CLI tool for downloading posts from Substack blogs and Substack Notes. It supports downloading individual posts or entire archives, with features for private newsletters (via cookies), rate limiting, format conversion (HTML/Markdown/Text), downloading of images and file attachments locally, creating archive index pages that link all downloaded posts with their metadata, and downloading Substack Notes for specific users.

## Architecture
The project follows a standard Go CLI structure:
- `main.go`: Entry point
- `cmd/`: Contains Cobra CLI commands (`root.go`, `download.go`, `list.go`, `version.go`, `notes.go`)
- `lib/`: Core library with five main components:
  - `fetcher.go`: HTTP client with rate limiting, retries, and cookie support
  - `extractor.go`: Post extraction and format conversion (HTML→Markdown/Text)
  - `images.go`: Image downloading and local path management
  - `files.go`: File attachment downloading and local path management
  - `notes.go`: Substack Notes downloading and API client

## Build and Development Commands

### Building
```bash
go build -o sbstck-dl .
```

### Running
```bash
go run . [command] [flags]
```

### Testing
```bash
go test ./...
go test ./lib
```

### Module management
```bash
go mod tidy
go mod download
```

## Key Components

### Fetcher (`lib/fetcher.go`)
- Handles HTTP requests with exponential backoff retry
- Rate limiting (default: 2 requests/second)
- Cookie support for private newsletters
- Proxy support

### Extractor (`lib/extractor.go`)
- Parses Substack post JSON from HTML
- Extracts post metadata including subtitle (.subtitle CSS selector) and cover image (og:image meta tag)
- Converts HTML to Markdown/Text using external libraries
- Handles file writing with different formats
- Provides archive page generation functionality (HTML/Markdown/Text formats)
- Manages archive entries with automatic sorting by publication date (newest first)

### Image Downloader (`lib/images.go`)
- Downloads images locally from Substack posts
- Supports multiple image quality levels (high/medium/low)
- Handles various Substack CDN URL patterns
- Updates HTML/Markdown content to reference local image paths
- Creates organized directory structure for downloaded images

### File Downloader (`lib/files.go`)
- Downloads file attachments from Substack posts using CSS selector `.file-embed-button.wide`
- Supports file extension filtering (optional)
- Creates organized directory structure for downloaded files
- Updates HTML content to reference local file paths
- Handles filename sanitization and collision avoidance
- Integrates with existing image download workflow

### Notes Client (`lib/notes.go`)
- Downloads Substack Notes via the user activity feed API
- Fetches activity across multiple pages with pagination support
- Filters comments to identify actual notes vs regular post comments
- Converts API response to structured note format with metadata
- Supports HTML, Markdown, and plain text output formats
- Organizes notes by timestamp with sanitized filenames
- Extracts post context, publication info, and engagement metrics

### Archive Page Generator (`lib/extractor.go`)
- Creates index pages linking all downloaded posts with metadata
- Supports HTML, Markdown, and Text formats matching the selected output format
- Includes post titles (linked to downloaded files with relative paths)
- Shows publication dates and download timestamps
- Displays post descriptions/subtitles and cover images when available
- Automatically sorts posts by publication date (newest first)
- Generates `index.{format}` in the output directory root

### Commands Structure
Uses Cobra framework:
- `download`: Main functionality for downloading posts
- `list`: Lists available posts from a Substack
- `notes`: Downloads Substack Notes for a specific user
- `version`: Shows version information

## Dependencies
- `github.com/spf13/cobra`: CLI framework
- `github.com/PuerkitoBio/goquery`: HTML parsing
- `github.com/JohannesKaufmann/html-to-markdown`: HTML to Markdown conversion
- `github.com/cenkalti/backoff/v4`: Exponential backoff for retries
- `golang.org/x/time/rate`: Rate limiting
- `golang.org/x/sync/errgroup`: Concurrent processing

## Common Development Tasks

### Running the CLI locally
```bash
go run . download --url https://example.substack.com --output ./downloads
```

### Testing with verbose output
```bash
go run . download --url https://example.substack.com --verbose --dry-run
```

### Downloading posts with images
```bash
# Download posts with high-quality images
go run . download --url https://example.substack.com --download-images --image-quality high --output ./downloads

# Download with medium quality images and custom images directory
go run . download --url https://example.substack.com --download-images --image-quality medium --images-dir assets --output ./downloads

# Download single post with images in markdown format
go run . download --url https://example.substack.com/p/post-title --download-images --format md --output ./downloads
```

### Downloading posts with file attachments
```bash
# Download posts with file attachments
go run . download --url https://example.substack.com --download-files --output ./downloads

# Download with specific file extensions only
go run . download --url https://example.substack.com --download-files --file-extensions "pdf,docx,txt" --output ./downloads

# Download with custom files directory name
go run . download --url https://example.substack.com --download-files --files-dir attachments --output ./downloads

# Download single post with both images and file attachments
go run . download --url https://example.substack.com/p/post-title --download-images --download-files --output ./downloads
```

### Creating archive index pages
```bash
# Download posts and create an archive index page
go run . download --url https://example.substack.com --create-archive --output ./downloads

# Download entire archive with archive index in markdown format
go run . download --url https://example.substack.com --create-archive --format md --output ./downloads

# Download single post with archive page (useful for building up an archive over time)
go run . download --url https://example.substack.com/p/post-title --create-archive --output ./downloads

# Download with all features: images, files, and archive page
go run . download --url https://example.substack.com --download-images --download-files --create-archive --output ./downloads

# Download archive with specific format and custom directories
go run . download --url https://example.substack.com --create-archive --format html --images-dir assets --files-dir attachments --output ./downloads
```

### Downloading Substack Notes
```bash
# Download notes for a specific user by user ID
go run . notes --user-id 303863305 --username nweiss --output-dir ./notes

# Download notes in HTML format
go run . notes --user-id 303863305 --format html --output-dir ./notes

# Download notes with filtering to get only actual notes (vs regular comments)
go run . notes --user-id 303863305 --notes-only --output-dir ./notes

# Download more pages of notes activity
go run . notes --user-id 303863305 --max-pages 20 --verbose --output-dir ./notes

# Download notes in plain text format
go run . notes --user-id 303863305 --format txt --output-dir ./notes
```

### Building for release
```bash
go build -ldflags="-s -w" -o sbstck-dl .
```