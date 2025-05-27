# Link Checker GitHub Action

A GitHub Action to check for broken links (4xx, 5xx status codes) on websites.
It supports both sitemap-based checking and web crawling.

## Features

- ✅ Check links from XML sitemaps
- 🕷️ Crawl websites to discover and check links
- 🚀 Concurrent link checking with rate limiting
- 🎯 Configurable exclude patterns (regex support)
- 📊 Detailed reporting with JSON output
- 🔧 Highly configurable

## Usage

### Using with Sitemap

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
        uses: ./
        with:
          sitemap-url: 'https://joshbeard.me/sitemap.xml'
          timeout: 30
          max-concurrent: 10
          exclude-patterns: '.*\.pdf$,.*example\.com.*'
```

### Using with Web Crawling

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
        uses: ./
        with:
          base-url: 'https://example.com'
          max-depth: 3
          timeout: 30
          max-concurrent: 5
          fail-on-error: true
```

### Complete Example with Error Handling

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
        uses: ./
        with:
          sitemap-url: 'https://joshbeard.me/sitemap.xml'
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

## Inputs

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

## Outputs

| Output | Description |
|--------|-------------|
| `broken-links-count` | Number of broken links found |
| `broken-links` | JSON array of broken links with details |
| `total-links-checked` | Total number of links checked |

## Examples

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

### Custom User Agent

```yaml
with:
  user-agent: 'MyWebsite-LinkChecker/1.0 (+https://example.com/about)'
```

### Rate Limiting

Control the number of concurrent requests to be respectful to the target server:

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

## Local Testing

You can test the action locally using Docker:

```bash
# Test with sitemap
docker build -t link-checker .
docker run --rm \
  -e INPUT_SITEMAP-URL=https://joshbeard.me/sitemap.xml \
  -e INPUT_TIMEOUT=30 \
  -e INPUT_MAX-CONCURRENT=5 \
  -e INPUT_VERBOSE=true \
  link-checker

# Test with crawling
docker run --rm \
  -e INPUT_BASE-URL=https://example.com \
  -e INPUT_MAX-DEPTH=2 \
  -e INPUT_TIMEOUT=30 \
  -e INPUT_VERBOSE=true \
  link-checker
```

## Development

This action is built with Go and uses:
- `golang.org/x/net/html` for HTML parsing
- `golang.org/x/time/rate` for rate limiting
- Standard library for HTTP requests and XML parsing

To build locally:

```bash
go mod tidy
go build -o link-checker ./cmd/link-checker
```

## License

MIT License - see LICENSE file for details.