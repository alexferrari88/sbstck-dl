# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview
This is a Go CLI tool for downloading posts from Substack blogs. It supports downloading individual posts or entire archives, with features for private newsletters (via cookies), rate limiting, format conversion (HTML/Markdown/Text), and downloading of images and file attachments locally.

## Architecture
The project follows a standard Go CLI structure:
- `main.go`: Entry point
- `cmd/`: Contains Cobra CLI commands (`root.go`, `download.go`, `list.go`, `version.go`)
- `lib/`: Core library with four main components:
  - `fetcher.go`: HTTP client with rate limiting, retries, and cookie support
  - `extractor.go`: Post extraction and format conversion (HTML→Markdown/Text)
  - `images.go`: Image downloading and local path management
  - `files.go`: File attachment downloading and local path management

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
- Converts HTML to Markdown/Text using external libraries
- Handles file writing with different formats

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

### Commands Structure
Uses Cobra framework:
- `download`: Main functionality for downloading posts
- `list`: Lists available posts from a Substack
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

### Building for release
```bash
go build -ldflags="-s -w" -o sbstck-dl .
```