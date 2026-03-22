# Contributing to TG WS Proxy Go

First off, thank you for considering contributing to TG WS Proxy Go!

## How Can I Contribute?

### Reporting Bugs

Before creating bug reports, please check the existing issues as you might find out that you don't need to create one. When you are creating a bug report, please include as many details as possible:

* **Use a clear and descriptive title**
* **Describe the exact steps to reproduce the problem**
* **Provide specific examples to demonstrate the steps**
* **Describe the behavior you observed and what behavior you expected**
* **Include logs if possible** (from %APPDATA%/TgWsProxy/proxy.log)

### Suggesting Enhancements

Enhancement suggestions are tracked as GitHub issues. When creating an enhancement suggestion, please include:

* **Use a clear and descriptive title**
* **Provide a detailed description of the suggested enhancement**
* **Explain why this enhancement would be useful**
* **List some examples of how this enhancement would be used**

### Pull Requests

* Fill in the required template
* Follow the Go style guide
* Include comments in your code where necessary
* Update documentation if needed

## Development Setup

### Prerequisites

* Go 1.21 or later
* Git

### Building

```bash
# Clone the repository
git clone https://github.com/y0sy4/tg-ws-proxy-go.git
cd tg-ws-proxy-go

# Build for your platform
go build -o TgWsProxy.exe ./cmd/proxy  # Windows
go build -o TgWsProxy_linux ./cmd/proxy  # Linux
go build -o TgWsProxy_macos ./cmd/proxy  # macOS
```

### Running Tests

```bash
go test -v ./internal/...
```

## Code Style

* Follow [Effective Go](https://golang.org/doc/effective_go)
* Use `gofmt` or `goimports` to format code
* Keep functions small and focused
* Add comments for exported functions

## Questions?

Feel free to open an issue for any questions!
