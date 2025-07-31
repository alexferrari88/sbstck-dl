# Project Overview

## Purpose
sbstck-dl is a Go CLI tool for downloading posts from Substack blogs. It supports downloading individual posts or entire archives, with features for private newsletters (via cookies), rate limiting, and format conversion (HTML/Markdown/Text). The tool also supports downloading images and file attachments locally.

## Tech Stack
- **Language**: Go 1.20+
- **CLI Framework**: Cobra (github.com/spf13/cobra)
- **HTML Parsing**: goquery (github.com/PuerkitoBio/goquery)
- **HTML to Markdown**: html-to-markdown (github.com/JohannesKaufmann/html-to-markdown)
- **HTML to Text**: html2text (github.com/k3a/html2text)
- **Retry Logic**: backoff (github.com/cenkalti/backoff/v4)
- **Rate Limiting**: golang.org/x/time/rate
- **Concurrency**: golang.org/x/sync/errgroup
- **Progress Bar**: progressbar (github.com/schollz/progressbar/v3)
- **Testing**: testify (github.com/stretchr/testify)

## Repository Structure
- `main.go`: Entry point
- `cmd/`: Cobra CLI commands (root.go, download.go, list.go, version.go)
- `lib/`: Core library components
  - `fetcher.go`: HTTP client with rate limiting, retries, and cookie support
  - `extractor.go`: Post extraction and format conversion (HTML→Markdown/Text)
  - `images.go`: Image downloading and local path management
  - `files.go`: File attachment downloading and local path management
- `.github/workflows/`: CI/CD workflows for testing and releases
- Tests are co-located with source files (e.g., `lib/fetcher_test.go`)