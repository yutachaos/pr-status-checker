# PR Status Checker

[![CI](https://github.com/yutachaos/pr-status-checker/actions/workflows/ci.yml/badge.svg)](https://github.com/yutachaos/pr-status-checker/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/yutachaos/pr-status-checker)](https://goreportcard.com/report/github.com/yutachaos/pr-status-checker)

A command-line tool that automatically checks and manages GitHub pull requests. It monitors the status of pull requests, updates branches when needed, and automatically merges them when all checks pass.

## Features

- Automatically checks status of open pull requests
- Updates branches that are behind the base branch
- Automatically merges pull requests when all status checks pass
- Supports both HTTPS and SSH GitHub repository URLs
- Concurrent processing of multiple pull requests

## Installation

```bash
go install github.com/yutachaos/pr-status-checker@latest
```

## Configuration

The tool can be configured using either command-line flags or environment variables:

### Command-line flags

- `-token`: GitHub personal access token
- `-owner`: Repository owner (username or organization)
- `-repo`: Repository name

### Environment variables

- `GITHUB_TOKEN`: GitHub personal access token
- `GITHUB_OWNER`: Repository owner (username or organization)
- `GITHUB_REPO`: Repository name

If owner and repo are not specified, the tool will attempt to detect them from the git configuration of the current directory.

## Usage

1. Set up your GitHub token:
```bash
export GITHUB_TOKEN="your-github-token"
```

2. Run in the current repository:
```bash
pr-status-checker
```

Or specify a different repository:
```bash
pr-status-checker -owner username -repo repository
```

## Requirements

- Go 1.23 or later
- GitHub Personal Access Token with appropriate permissions:
  - `repo` scope for private repositories
  - `public_repo` scope for public repositories

## Development

1. Clone the repository:
```bash
git clone https://github.com/yutachaos/pr-status-checker.git
```

2. Install dependencies:
```bash
go mod download
```

3. Run tests:
```bash
go test -v ./...
```

## License

This project is licensed under the MIT License - see the LICENSE file for details. 