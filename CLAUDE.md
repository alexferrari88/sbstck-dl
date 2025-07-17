# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview
This is a Go CLI tool for downloading posts from Substack blogs. It supports downloading individual posts or entire archives, with features for private newsletters (via cookies), rate limiting, and format conversion (HTML/Markdown/Text).

## Architecture
The project follows a standard Go CLI structure:
- `main.go`: Entry point
- `cmd/`: Contains Cobra CLI commands (`root.go`, `download.go`, `list.go`, `version.go`)
- `lib/`: Core library with two main components:
  - `fetcher.go`: HTTP client with rate limiting, retries, and cookie support
  - `extractor.go`: Post extraction and format conversion (HTML→Markdown/Text)

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

### Building for release
```bash
go build -ldflags="-s -w" -o sbstck-dl .
```