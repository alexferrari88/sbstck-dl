# File Attachment Download Feature Specification

## 1. Overview

### 1.1 Purpose
Add support for downloading file attachments from Substack posts alongside the existing text and image download functionality. This feature will enable users to download PDFs, documents, and other files that authors embed in their posts, with local file references updated in the downloaded content.

### 1.2 Success Criteria
- Users can download file attachments from Substack posts using command-line flags
- Downloaded files are organized in a configurable directory structure
- HTML/Markdown content is updated with local file paths
- Optional file extension filtering allows selective downloading
- Integration with existing rate limiting and retry mechanisms
- Comprehensive error handling for network failures and unsupported file types

### 1.3 Scope Boundaries
**In Scope:**
- Detection and extraction of file attachment URLs from Substack HTML
- Download of attachments with appropriate file naming
- Content rewriting to reference local file paths
- File extension filtering capabilities
- Integration with existing fetcher infrastructure
- Support for all common file types (PDF, DOC, TXT, etc.)

**Out of Scope:**
- File preview or content analysis capabilities
- Automatic file conversion between formats
- Virus scanning or security validation of downloaded files
- Selective downloading based on file size limits
- Cloud storage integration for downloaded files

## 2. Technical Architecture

### 2.1 Architecture Alignment
This feature follows the established sbstck-dl patterns:
- **Modular Design**: New `FileDownloader` struct similar to existing `ImageDownloader`
- **Consistent Interface**: Integration with existing CLI flags and output patterns
- **Error Handling**: Leverages existing retry and backoff mechanisms from `Fetcher`
- **Content Rewriting**: Similar approach to image URL replacement in HTML/Markdown

### 2.2 Core Components

#### 2.2.1 FileDownloader Struct
```go
type FileDownloader struct {
    fetcher     *Fetcher
    outputDir   string
    filesDir    string
    allowedExts []string // empty means all extensions allowed
}
```

#### 2.2.2 File Information Structure
```go
type FileInfo struct {
    URL         string
    Filename    string
    Extension   string
    Size        string
    Type        string
    LocalPath   string
}

type FileDownloadResult struct {
    Files       []FileInfo
    UpdatedHTML string
    Errors      []error
}
```

### 2.3 HTML Parsing Strategy

#### 2.3.1 CSS Selector Target
- **Primary Selector**: `.file-embed-button.wide`
- **Container Selector**: `.file-embed-container-top` (for metadata extraction)

#### 2.3.2 HTML Structure Analysis
Based on the example URL, file attachments follow this structure:
```html
<div class="file-embed-container-top">
    <img src="..." class="file-embed-thumbnail-default">
    <div class="file-embed-details">
        <div class="file-embed-details-h1">The Stone Boy Cropped 1</div>
        <div class="file-embed-details-h2">207KB ∙ PDF file</div>
    </div>
    <a href="https://georgesaunders.substack.com/api/v1/file/..." 
       class="file-embed-button wide">
        <span class="file-embed-button-text">Download</span>
    </a>
</div>
```

## 3. Command Line Interface

### 3.1 New CLI Flags

```go
// New flags to add to cmd/download.go
var (
    downloadFiles    bool     // --download-files
    filesDir         string   // --files-dir  
    allowedFileExts  []string // --file-extensions
)
```

### 3.2 Flag Definitions

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--download-files` | | `false` | Download file attachments locally and update content references |
| `--files-dir` | | `"files"` | Directory name for downloaded files (relative to output directory) |
| `--file-extensions` | | `[]` (all) | Comma-separated list of allowed file extensions (e.g., "pdf,doc,txt") |

### 3.3 Usage Examples

```bash
# Download posts with all file attachments
sbstck-dl download --url https://example.substack.com --download-files

# Download only PDF and DOC files to custom directory
sbstck-dl download --url https://example.substack.com --download-files \
    --file-extensions "pdf,doc" --files-dir "documents"

# Combined with existing features
sbstck-dl download --url https://example.substack.com --download-files \
    --download-images --format md --output ./downloads
```

## 4. Implementation Details

### 4.1 File Detection Algorithm

1. **HTML Parsing**: Use goquery to find all `.file-embed-button.wide` elements
2. **URL Extraction**: Extract `href` attribute from anchor tags
3. **Metadata Extraction**: Parse container for filename, size, and type information
4. **Extension Filtering**: Apply user-specified extension filters if provided

### 4.2 File Naming Strategy

```go
func (fd *FileDownloader) generateSafeFilename(fileInfo FileInfo, index int) string {
    // Priority order for filename:
    // 1. Extract from file-embed-details-h1 if available
    // 2. Parse from URL path
    // 3. Generate from URL hash + extension
    // 4. Fallback: "attachment_<index>.<ext>"
}
```

### 4.3 Content Rewriting

#### 4.3.1 HTML Content Updates
- Replace `href` attributes in `.file-embed-button.wide` elements
- Maintain original HTML structure while updating file paths
- Handle both absolute and relative path scenarios

#### 4.3.2 Markdown Content Updates
- Convert file embed HTML to Markdown link format: `[filename](local/path)`
- Preserve file metadata information in link text when possible

### 4.4 Directory Structure

```
output_directory/
├── post-title.html
├── images/           # existing images directory
│   └── image1.jpg
└── files/           # new files directory
    ├── document1.pdf
    ├── spreadsheet1.xlsx
    └── archive1.zip
```

## 5. Integration Points

### 5.1 Extractor Integration

```go
// Add to Post struct
type Post struct {
    // ... existing fields
    FileDownloadResult *FileDownloadResult `json:"file_download_result,omitempty"`
}

// New method on Post
func (p *Post) WriteToFileWithAttachments(ctx context.Context, path, format string, 
    addSourceURL, downloadImages, downloadFiles bool, imageQuality ImageQuality, 
    imagesDir, filesDir string, allowedExts []string, fetcher *Fetcher) (*FileDownloadResult, error)
```

### 5.2 Command Integration

```go
// Update in cmd/download.go init()
downloadCmd.Flags().BoolVar(&downloadFiles, "download-files", false, 
    "Download file attachments locally and update content to reference local files")
downloadCmd.Flags().StringVar(&filesDir, "files-dir", "files", 
    "Directory name for downloaded files")
downloadCmd.Flags().StringSliceVar(&allowedFileExts, "file-extensions", []string{}, 
    "Comma-separated list of allowed file extensions (empty = all extensions)")
```

## 6. Error Handling Strategy

### 6.1 Network Error Handling
- **Retry Logic**: Leverage existing `Fetcher` retry mechanisms with exponential backoff
- **Rate Limiting**: Respect existing rate limiting for file downloads
- **Timeout Handling**: Use configurable timeouts for large file downloads

### 6.2 File System Error Handling
- **Directory Creation**: Ensure files directory exists before downloading
- **Permission Errors**: Graceful handling of write permission issues
- **Disk Space**: Basic validation for available disk space

### 6.3 Content Error Handling
- **Invalid URLs**: Skip malformed or inaccessible file URLs
- **Extension Filtering**: Log filtered files for user awareness
- **Partial Failures**: Continue processing other files if individual downloads fail

## 7. Performance Considerations

### 7.1 Concurrent Downloads
- Use Go's `errgroup` pattern consistent with existing image download implementation
- Configurable worker pools to prevent resource exhaustion
- Progress reporting for large file downloads

### 7.2 Memory Management
- Stream large files to disk rather than loading entirely in memory
- Implement file size limits to prevent excessive memory usage
- Clean up temporary files on process interruption

## 8. Testing Strategy

### 8.1 Unit Tests

```go
// Test coverage areas
func TestFileDownloader_ExtractFileElements(t *testing.T)
func TestFileDownloader_GenerateSafeFilename(t *testing.T)  
func TestFileDownloader_DownloadSingleFile(t *testing.T)
func TestFileDownloader_UpdateHTMLWithLocalPaths(t *testing.T)
func TestFileDownloader_ExtensionFiltering(t *testing.T)
```

### 8.2 Integration Tests
- **Real Substack Posts**: Test with actual posts containing file attachments
- **Network Conditions**: Test behavior under various network conditions
- **File Type Coverage**: Test common file types (PDF, DOC, XLS, ZIP, etc.)
- **Edge Cases**: Empty responses, malformed HTML, missing files

### 8.3 Performance Tests
- **Large File Handling**: Test download of files >100MB
- **Multiple Files**: Test posts with many attachments
- **Concurrent Processing**: Validate worker pool behavior

## 9. Security Considerations

### 9.1 File Path Security
- **Path Traversal Prevention**: Sanitize filenames to prevent directory traversal attacks
- **Safe Filename Generation**: Remove or escape dangerous characters in filenames
- **Directory Containment**: Ensure all downloads remain within designated directories

### 9.2 Content Validation
- **URL Validation**: Validate file URLs are from expected Substack domains
- **File Type Validation**: Basic MIME type checking for downloaded files
- **Size Limits**: Implement reasonable file size limits to prevent abuse

## 10. Migration and Rollout

### 10.1 Backward Compatibility
- New feature is entirely opt-in via `--download-files` flag
- No changes to existing CLI behavior when flag is not used
- Existing configurations and scripts remain unaffected

### 10.2 Documentation Updates
- Update CLI help text and documentation
- Add usage examples to README
- Document new directory structure and file naming conventions

## 11. Future Enhancements

### 11.1 Potential Extensions
- **File Size Filtering**: Add flags for minimum/maximum file size limits
- **Content Type Detection**: Enhanced MIME type detection and handling
- **Progress Indicators**: Visual progress bars for large downloads
- **Deduplication**: Skip downloading identical files across multiple posts

### 11.2 Advanced Features
- **Selective Downloads**: Interactive mode for choosing which files to download
- **Metadata Preservation**: Store original file metadata in sidecar files
- **Cloud Integration**: Optional upload to cloud storage services

---

**Specification Status**: Draft v1.0  
**Last Updated**: 2025-07-31  
**Dependencies**: Existing sbstck-dl codebase (fetcher.go, extractor.go, images.go)