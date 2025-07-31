# Code Style and Conventions

## Go Style Guidelines
- Follows standard Go conventions and formatting
- Uses `gofmt` for code formatting
- Package naming: lowercase, single words when possible
- Function naming: CamelCase for exported, camelCase for unexported
- Variable naming: camelCase, descriptive names

## Code Organization
- **Separation of Concerns**: CLI logic in `cmd/`, core business logic in `lib/`
- **Error Handling**: Explicit error returns, wrapping with context using `fmt.Errorf`
- **Testing**: Table-driven tests, benchmarks for performance-critical code
- **Concurrency**: Uses errgroup for managed goroutines, context for cancellation

## Naming Conventions
- **Structs**: PascalCase (e.g., `FileDownloader`, `ImageInfo`)
- **Interfaces**: Usually end with -er (e.g., implied by method names)
- **Constants**: PascalCase for exported, camelCase for unexported
- **Files**: snake_case for test files (`*_test.go`)

## Function Design Patterns
- **Constructor Pattern**: `NewXxx()` functions for creating instances
- **Options Pattern**: Used in fetcher with `FetcherOption` functional options
- **Context Propagation**: All network operations accept `context.Context`
- **Resource Management**: Proper `defer` usage for cleanup (file handles, HTTP responses)

## Documentation
- **Godoc Comments**: All exported functions, types, and constants have comments
- **README**: Comprehensive usage examples and feature documentation
- **Code Comments**: Explain complex logic, especially in parsing and URL manipulation