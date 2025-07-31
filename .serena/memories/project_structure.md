# Project Structure - sbstck-dl

## Overview
Go CLI tool for downloading posts from Substack blogs with support for private newsletters, rate limiting, and format conversion.

## Directory Structure
```
├── main.go              # Entry point
├── cmd/                 # Cobra CLI commands
│   ├── root.go
│   ├── download.go      # Main download functionality
│   ├── list.go
│   ├── version.go
│   ├── cmd_test.go      # Command tests
│   └── integration_test.go
├── lib/                 # Core library
│   ├── fetcher.go       # HTTP client with rate limiting/retries
│   ├── fetcher_test.go  # Comprehensive HTTP client tests
│   ├── extractor.go     # Post extraction and format conversion
│   ├── extractor_test.go # Extractor tests
│   ├── images.go        # Image downloader
│   ├── images_test.go   # Comprehensive image tests
│   └── files.go         # NEW: File attachment downloader
└── go.mod               # Dependencies
```

## Key Dependencies
- `github.com/spf13/cobra` - CLI framework
- `github.com/PuerkitoBio/goquery` - HTML parsing
- `github.com/stretchr/testify` - Testing framework
- `github.com/cenkalti/backoff/v4` - Exponential backoff
- `golang.org/x/time/rate` - Rate limiting