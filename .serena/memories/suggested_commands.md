# Suggested Commands

## Development Commands

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
# Run all tests
go test ./...

# Run tests with verbose output
go test -v ./...

# Run tests for specific package
go test ./lib
go test ./cmd
```

### Module Management
```bash
# Clean up dependencies
go mod tidy

# Download dependencies
go mod download

# Verify dependencies
go mod verify
```

### Running the CLI Locally
```bash
# Download single post
go run . download --url https://example.substack.com/p/post-title --output ./downloads

# Download entire archive
go run . download --url https://example.substack.com --output ./downloads

# Download with images
go run . download --url https://example.substack.com --download-images --output ./downloads

# Download with file attachments
go run . download --url https://example.substack.com --download-files --output ./downloads

# Download with both images and files
go run . download --url https://example.substack.com --download-images --download-files --output ./downloads

# Test with dry run and verbose output
go run . download --url https://example.substack.com --verbose --dry-run
```

### System Commands (Linux)
- `rg` (ripgrep) for searching instead of grep
- Standard Linux commands: `ls`, `cd`, `find`, `git`