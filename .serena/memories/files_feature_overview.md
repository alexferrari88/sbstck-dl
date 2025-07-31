# File Attachment Download Feature

## Implementation Overview
New feature added in `lib/files.go` that allows downloading file attachments from Substack posts.

## Key Components

### FileDownloader struct
- Manages file downloads with rate limiting via Fetcher
- Configurable output directory and file extensions filter
- Integrates with existing image download workflow

### CSS Selector Detection
- Uses `.file-embed-button.wide` to find file attachment links
- Extracts download URLs from `href` attributes

### Core Functions
- `DownloadFiles()` - Main entry point, returns FileDownloadResult
- `extractFileElements()` - Finds file links in HTML using CSS selector
- `downloadSingleFile()` - Downloads individual files with error handling
- `updateHTMLWithLocalPaths()` - Replaces URLs with local paths

### Features
- Extension filtering via `--file-extensions` flag
- Custom output directory via `--files-dir` flag
- Filename extraction from URLs and query parameters
- Safe filename sanitization (removes unsafe characters)
- File existence checking (skip if already downloaded)
- Relative path conversion for HTML references

## CLI Integration
- New flags in `cmd/download.go`:
  - `--download-files` - Enable file downloading
  - `--file-extensions` - Filter by extensions (comma-separated)
  - `--files-dir` - Custom files directory name

## Integration with Extractor
- Extended `WriteToFileWithImages()` to also handle file downloads
- Unified workflow for both images and files