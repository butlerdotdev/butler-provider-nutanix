# Contributing to Butler Provider Nutanix

Thank you for your interest in contributing to Butler! This document provides guidelines for contributing.

## Getting Started

1. Fork the repository
2. Clone your fork
3. Create a feature branch
4. Make your changes
5. Submit a pull request

## Development Setup

```bash
# Clone the repository
git clone https://github.com/butlerdotdev/butler-provider-nutanix.git
cd butler-provider-nutanix

# Install dependencies
go mod download

# Build
make build

# Run tests
make test
```

## Code Standards

- Follow Go conventions and idioms
- Use `gofmt` and `goimports`
- Write tests for new functionality
- Document exported functions

## Pull Request Process

1. Ensure your code builds and tests pass
2. Update documentation as needed
3. Sign your commits (DCO)
4. Submit PR against `main` branch

## Signing Commits

We require all commits to be signed off per the Developer Certificate of Origin (DCO):

```bash
git commit -s -m "Your commit message"
```

## Questions?

Open an issue for discussion.
