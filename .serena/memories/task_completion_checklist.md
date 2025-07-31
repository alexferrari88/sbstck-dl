# Task Completion Checklist

## After Completing Development Tasks

### Testing
1. **Run Unit Tests**: `go test ./...`
2. **Run Integration Tests**: `go test -v ./...` 
3. **Test CLI Commands**: Manual testing with real Substack URLs
4. **Test Edge Cases**: Error conditions, malformed URLs, network failures

### Code Quality
1. **Format Code**: `gofmt -w .` (usually handled by editor)
2. **Lint Code**: Use `golint` or `go vet` if available
3. **Verify Dependencies**: `go mod tidy && go mod verify`

### Documentation Updates
1. **Update CLAUDE.md**: Add new features, commands, architectural changes
2. **Update README.md**: Add usage examples for new features
3. **Update Help Text**: Ensure CLI help reflects new flags and options
4. **Update Comments**: Ensure godoc comments are current

### Version Control
1. **Stage Changes**: `git add` only relevant files
2. **Commit**: Use conventional commits format
   - `feat: add new feature`
   - `fix: resolve bug`
   - `docs: update documentation`
   - `test: add tests`
   - `refactor: improve code structure`
3. **Clean Up**: Remove any temporary files or test artifacts

### Build Verification
1. **Build Binary**: `go build -o sbstck-dl .`
2. **Test Binary**: Run basic commands to ensure it works
3. **Cross-Platform Check**: Ensure no platform-specific code issues