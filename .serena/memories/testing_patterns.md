# Testing Patterns in sbstck-dl

## Test Structure
- All tests use `github.com/stretchr/testify` with `assert` and `require`
- Tests organized in table-driven style where appropriate
- Each major component has comprehensive test coverage

## Common Patterns

### HTTP Server Tests
- Use `httptest.NewServer()` for mock servers
- Test various response scenarios (success, errors, timeouts)
- Handle concurrent requests and rate limiting

### File I/O Tests
- Use `os.MkdirTemp()` for temporary directories
- Always clean up with `defer os.RemoveAll(tempDir)`
- Test file creation, existence, and content validation

### HTML Parsing Tests
- Use `goquery.NewDocumentFromReader(strings.NewReader(html))`
- Test various HTML structures and edge cases
- Validate URL extraction and replacement

### Error Handling Tests
- Test both success and failure scenarios
- Use specific error assertions and error message checking
- Test context cancellation and timeouts

### Benchmark Tests
- Include performance benchmarks for critical paths
- Use `b.ResetTimer()` appropriately
- Test both single operations and concurrent scenarios

## Test Organization
- Unit tests for individual functions
- Integration tests for complete workflows
- Regression tests for specific bug fixes
- Real-world data tests (when sample data available)