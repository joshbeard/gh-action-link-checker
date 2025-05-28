# Link Validator

A tool and GitHub Action to check for broken links (4xx, 5xx status codes) on websites.
It supports both sitemap-based checking and web crawling.

## Features

- **Sitemap Support**: Check links from XML sitemaps
- **Website Crawling**: Recursively crawl websites to discover links
- **Concurrent Processing**: Configurable concurrent request limits for performance
- **Flexible Configuration**: Support for both command-line flags and environment variables
- **Pattern Exclusion**: Exclude URLs using regex patterns
- **GitHub Action Integration**: Built-in support for GitHub Actions with proper outputs
- **Dynamic URL Resolution**: Intelligent base URL detection using HTTP Content-Type headers
- **Comprehensive Reporting**: Detailed results with status codes, errors, and timing information
- **Help and Version Support**: Built-in help and version information

## Installation & Usage

### GitHub Action

Use directly in your GitHub workflows:

```yaml
- name: Check links
  uses: joshbeard/gh-action-link-checker@v1
  with:
    sitemap-url: 'https://example.com/sitemap.xml'
```

### Docker Image

Available on GitHub Container Registry and Docker Hub:

```bash
# From GitHub Container Registry
docker run --rm ghcr.io/joshbeard/link-checker:latest \
  --sitemap-url https://example.com/sitemap.xml

# From Docker Hub
docker run --rm joshbeard/link-checker:latest \
  --sitemap-url https://example.com/sitemap.xml
```

### Binary Releases

Download pre-built binaries from [GitHub Releases](https://github.com/joshbeard/gh-action-link-checker/releases):

```bash
curl -L https://github.com/joshbeard/gh-action-link-checker/releases/latest/download/link-checker-linux-amd64 -o link-checker
chmod +x link-checker
./link-checker --sitemap-url https://example.com/sitemap.xml
```

### Getting Help

```bash
# Show help information
./link-checker --help

# Show version information
./link-checker --version
```

## Examples

### GitHub Action - Sitemap

```yaml
name: Check Links
on:
  schedule:
    - cron: '0 0 * * 0'  # Weekly on Sunday
  workflow_dispatch:

jobs:
  link-check:
    runs-on: ubuntu-latest
    steps:
      - name: Check links from sitemap
        uses: joshbeard/gh-action-link-checker@v1
        with:
          sitemap-url: 'https://example.com/sitemap.xml'
          timeout: 30
          max-concurrent: 10
          exclude-patterns: '.*\.pdf$,.*example\.com.*'
```

### GitHub Action - Web Crawling

```yaml
name: Check Links
on:
  push:
    branches: [main]

jobs:
  link-check:
    runs-on: ubuntu-latest
    steps:
      - name: Check links by crawling
        uses: joshbeard/gh-action-link-checker@v1
        with:
          base-url: 'https://example.com'
          max-depth: 3
          timeout: 30
          max-concurrent: 5
          fail-on-error: true
```

### GitLab CI

```yaml
link-check:
  stage: test
  image: ghcr.io/joshbeard/link-checker:latest
  script:
    - link-checker --sitemap-url https://example.com/sitemap.xml --timeout 30 --max-concurrent 5
  rules:
    - if: $CI_PIPELINE_SOURCE == "schedule"
    - if: $CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH
```

### Docker with Custom Configuration

```bash
docker run --rm ghcr.io/joshbeard/link-checker:latest \
  --base-url https://example.com \
  --max-depth 2 \
  --timeout 60 \
  --exclude-patterns ".*\.pdf$,.*\.zip$" \
  --verbose
```

### Complete GitHub Action with Error Handling

```yaml
name: Link Checker
on:
  schedule:
    - cron: '0 2 * * 1'  # Weekly on Monday at 2 AM
  workflow_dispatch:

jobs:
  check-links:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout
        uses: actions/checkout@v4

      - name: Check website links
        id: link-check
        uses: joshbeard/gh-action-link-checker@v1
        with:
          sitemap-url: 'https://example.com/sitemap.xml'
          timeout: 30
          user-agent: 'MyBot/1.0'
          exclude-patterns: '.*\.pdf$,.*\.zip$,.*example\.com.*'
          max-concurrent: 10
          fail-on-error: false

      - name: Comment on PR if broken links found
        if: steps.link-check.outputs.broken-links-count > 0
        uses: actions/github-script@v7
        with:
          script: |
            const brokenLinks = JSON.parse('${{ steps.link-check.outputs.broken-links }}');
            const count = '${{ steps.link-check.outputs.broken-links-count }}';

            let comment = `## 🔗 Link Check Results\n\n`;
            comment += `Found ${count} broken link(s):\n\n`;

            brokenLinks.forEach(link => {
              comment += `- ❌ [${link.url}](${link.url}) - ${link.error}\n`;
            });

            console.log(comment);
```

## Configuration

### Inputs (GitHub Action)

| Input | Description | Required | Default |
|-------|-------------|----------|---------|
| `sitemap-url` | URL to sitemap.xml to check links from | No | - |
| `base-url` | Base URL to crawl for links (used if sitemap-url not provided) | No | - |
| `max-depth` | Maximum crawl depth when using base-url | No | `3` |
| `timeout` | Request timeout in seconds | No | `30` |
| `user-agent` | User agent string for requests | No | `GitHub-Action-Link-Checker/1.0` |
| `exclude-patterns` | Comma-separated list of URL patterns to exclude (regex supported) | No | - |
| `fail-on-error` | Whether to fail the action if broken links are found | No | `true` |
| `max-concurrent` | Maximum number of concurrent requests | No | `10` |
| `verbose` | Show detailed output for each link checked | No | `false` |

### Command Line Flags

When using the binary or Docker image, use these flags:

```bash
-sitemap-url string       URL to sitemap.xml
-base-url string          Base URL to crawl
-max-depth int            Maximum crawl depth (default 3)
-timeout int              Request timeout in seconds (default 30)
-user-agent string        User agent string (default "GitHub-Action-Link-Checker/1.0")
-exclude-patterns string  Comma-separated exclude patterns
-max-concurrent int       Max concurrent requests (default 10)
-fail-on-error           Exit with error code if broken links found (default true)
-verbose                 Show detailed output
-help                    Show help information
-version                 Show version information
```

### Environment Variables

The tool supports environment variables (primarily for GitHub Action integration):

```bash
INPUT_SITEMAP_URL         URL of the sitemap to check
INPUT_BASE_URL            Base URL to start crawling from
INPUT_MAX_DEPTH           Maximum crawl depth (default: 3)
INPUT_TIMEOUT             Request timeout in seconds (default: 30)
INPUT_USER_AGENT          User agent string (default: Link-Validator/1.0)
INPUT_EXCLUDE_PATTERNS    Comma-separated regex patterns to exclude URLs
INPUT_FAIL_ON_ERROR       Exit with error code if broken links found (default: true)
INPUT_MAX_CONCURRENT      Maximum concurrent requests (default: 10)
INPUT_VERBOSE             Enable verbose output (default: false)
```

**Note**: Command line flags take precedence over environment variables.

### Outputs (GitHub Action)

| Output | Description |
|--------|-------------|
| `broken-links-count` | Number of broken links found |
| `broken-links` | JSON array of broken links with details |
| `total-links-checked` | Total number of links checked |

## Advanced Usage

### Using Environment Variables

You can use environment variables instead of command line flags:

```bash
# Check links from sitemap using environment variables
INPUT_SITEMAP_URL=https://example.com/sitemap.xml ./link-checker

# Crawl website using environment variables
INPUT_BASE_URL=https://example.com INPUT_MAX_DEPTH=2 INPUT_VERBOSE=true ./link-checker
```

### Exclude Patterns

You can exclude URLs using regex patterns:

```yaml
with:
  exclude-patterns: '.*\.pdf$,.*\.zip$,.*example\.com.*,.*#.*'
```

This will exclude:
- PDF files
- ZIP files
- Any URLs containing "example.com"
- Any URLs with fragments (anchors)

### Rate Limiting

Control concurrent requests to be respectful to target servers:

```yaml
with:
  max-concurrent: 5  # Only 5 concurrent requests
  timeout: 60        # 60 second timeout per request
```

### Verbose Output

Enable detailed output to see each link as it's being checked:

```yaml
with:
  verbose: true
```

This will show output like:
```
✅ [1/111] https://example.com/page1 (Status: 200, Duration: 45ms)
❌ [2/111] https://example.com/broken (Status: 404, Duration: 23ms)
🔄 [3/111] https://example.com/redirect (Status: 301, Duration: 67ms)
```

Status emojis:
- ✅ Success (2xx)
- 🔄 Redirect (3xx)
- ❌ Client Error (4xx)
- 💥 Server Error (5xx)
- ❓ Unknown/Error

## Development

### Building

```bash
go mod tidy
go build -o link-checker ./cmd/link-checker
```

Or use the Makefile:

```bash
make build    # Build the binary
make test     # Run tests
make help     # See all available targets
```

### Testing

Run the test suite:

```bash
go test ./...              # Run all tests
go test ./... -cover       # Run with coverage
go test ./... -v           # Verbose output
```

### Test Coverage

The project maintains high test coverage. To generate a coverage report:

```bash
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out -o coverage.html
```

## Dynamic URL Resolution

The link checker uses intelligent URL resolution to properly handle relative links on web pages:

1. **HTML Base Tag Detection**: If a page contains a `<base href="...">` tag, it uses that as the base URL for resolving relative links.

2. **Dynamic Content-Type Analysis**: When no base tag is present, the tool makes HTTP HEAD requests to determine if a URL represents a file or directory based on the Content-Type header:
   - **Directory-like content** (`text/html`, `application/json`, `application/xml`): Treats the URL as a directory for relative link resolution
   - **File-like content** (`application/pdf`, `image/*`, `audio/*`, `video/*`, etc.): Uses the parent directory for relative link resolution

3. **Extension-based Fallback**: If HTTP detection fails, falls back to file extension analysis to determine URL type.

## License

MIT License - see LICENSE file for details.