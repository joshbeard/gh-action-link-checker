package checker

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/joshbeard/link-validator/internal/config"
	"golang.org/x/net/html"
	"golang.org/x/time/rate"
)

// LinkResult represents the result of checking a single link
type LinkResult struct {
	URL        string `json:"url"`
	StatusCode int    `json:"status_code"`
	Error      string `json:"error,omitempty"`
	Duration   string `json:"duration"`
}

// Checker handles link checking operations
type Checker struct {
	config  *config.Config
	client  *http.Client
	limiter *rate.Limiter
}

// Sitemap represents the XML structure of a sitemap
type Sitemap struct {
	XMLName xml.Name `xml:"urlset"`
	URLs    []struct {
		Loc string `xml:"loc"`
	} `xml:"url"`
}

// New creates a new Checker instance
func New(cfg *config.Config) *Checker {
	client := &http.Client{
		Timeout: cfg.Timeout,
	}

	// Rate limiter to be respectful
	limiter := rate.NewLimiter(rate.Limit(cfg.MaxConcurrent), cfg.MaxConcurrent)

	return &Checker{
		config:  cfg,
		client:  client,
		limiter: limiter,
	}
}

// GetURLsFromSitemap fetches and parses a sitemap to extract URLs
func (c *Checker) GetURLsFromSitemap(sitemapURL string) ([]string, error) {
	req, err := http.NewRequest("GET", sitemapURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("User-Agent", c.config.UserAgent)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching sitemap: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sitemap returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading sitemap: %w", err)
	}

	var sitemap Sitemap
	if err := xml.Unmarshal(body, &sitemap); err != nil {
		return nil, fmt.Errorf("parsing sitemap XML: %w", err)
	}

	urls := make([]string, 0, len(sitemap.URLs))
	for _, urlEntry := range sitemap.URLs {
		if !c.shouldExclude(urlEntry.Loc) {
			urls = append(urls, urlEntry.Loc)
		}
	}

	return urls, nil
}

// CrawlWebsite crawls a website starting from baseURL up to maxDepth
func (c *Checker) CrawlWebsite(baseURL string, maxDepth int) ([]string, error) {
	visited := make(map[string]bool)
	var urls []string
	var mu sync.Mutex

	baseURLParsed, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parsing base URL: %w", err)
	}

	var crawl func(string, int)
	crawl = func(currentURL string, depth int) {
		if depth > maxDepth {
			return
		}

		mu.Lock()
		if visited[currentURL] {
			mu.Unlock()
			return
		}
		visited[currentURL] = true
		urls = append(urls, currentURL)
		if c.config.Verbose {
			fmt.Printf("Crawling [depth %d]: %s\n", depth, currentURL)
		}
		mu.Unlock()

		if depth == maxDepth {
			return
		}

		// Parse the current URL to use as base for relative link resolution
		currentURLParsed, err := url.Parse(currentURL)
		if err != nil {
			if c.config.Verbose {
				fmt.Printf("Error parsing current URL %s: %v\n", currentURL, err)
			}
			return
		}

		links, err := c.extractLinksFromPage(currentURL, currentURLParsed, baseURLParsed)
		if err != nil {
			if c.config.Verbose {
				fmt.Printf("Error extracting links from %s: %v\n", currentURL, err)
			}
			return
		}

		if c.config.Verbose && len(links) > 0 {
			fmt.Printf("Found %d links on %s\n", len(links), currentURL)
		}

		for _, link := range links {
			if !visited[link] && !c.shouldExclude(link) {
				crawl(link, depth+1)
			}
		}
	}

	crawl(baseURL, 0)
	return urls, nil
}

// extractLinksFromPage extracts all links from a web page
func (c *Checker) extractLinksFromPage(pageURL string, currentURL *url.URL, baseURL *url.URL) ([]string, error) {
	req, err := http.NewRequest("GET", pageURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.config.UserAgent)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("page returned status %d", resp.StatusCode)
	}

	doc, err := html.Parse(resp.Body)
	if err != nil {
		return nil, err
	}

	// Look for <base> tag to determine the correct base URL for this page
	resolveBaseURL := currentURL
	var findBase func(*html.Node)
	findBase = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "base" {
			for _, attr := range n.Attr {
				if attr.Key == "href" {
					if baseHref, err := url.Parse(attr.Val); err == nil {
						// Resolve the base href relative to the current URL
						resolveBaseURL = currentURL.ResolveReference(baseHref)
						if c.config.Verbose {
							fmt.Printf("Found base tag on %s: %s\n", pageURL, resolveBaseURL.String())
						}
					}
					break
				}
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			findBase(child)
		}
	}
	findBase(doc)

	// If no base tag was found, we need to determine the appropriate base URL
	// for resolving relative links. If the current URL doesn't end with a slash
	// and doesn't have a file extension, treat it as a directory.
	if resolveBaseURL == currentURL {
		resolveBaseURL = c.getResolveBaseURL(currentURL)
		if c.config.Verbose && resolveBaseURL.String() != currentURL.String() {
			fmt.Printf("No base tag found, using directory-based resolution: %s\n", resolveBaseURL.String())
		}
	}

	var links []string
	var extract func(*html.Node)
	extract = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			for _, attr := range n.Attr {
				if attr.Key == "href" {
					link := attr.Val
					if absoluteURL := c.resolveURL(link, resolveBaseURL); absoluteURL != "" {
						// Only include links from the same domain
						if linkURL, err := url.Parse(absoluteURL); err == nil {
							if linkURL.Host == baseURL.Host {
								links = append(links, absoluteURL)
							}
						}
					}
					break
				}
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			extract(child)
		}
	}

	extract(doc)
	return links, nil
}

// resolveURL converts relative URLs to absolute URLs
func (c *Checker) resolveURL(href string, baseURL *url.URL) string {
	if href == "" || strings.HasPrefix(href, "#") || strings.HasPrefix(href, "javascript:") || strings.HasPrefix(href, "mailto:") {
		return ""
	}

	linkURL, err := url.Parse(href)
	if err != nil {
		return ""
	}

	return baseURL.ResolveReference(linkURL).String()
}

// getResolveBaseURL determines the appropriate base URL for resolving relative links
// when no <base> tag is present. It uses HTTP Content-Type headers and URL path analysis
// to determine if the URL represents a file or directory.
func (c *Checker) getResolveBaseURL(currentURL *url.URL) *url.URL {
	// If the URL already ends with a slash, it's already a directory
	if strings.HasSuffix(currentURL.Path, "/") {
		return currentURL
	}

	// First, check if the URL has a file extension - this is the most reliable indicator
	pathSegments := strings.Split(currentURL.Path, "/")
	if len(pathSegments) > 0 {
		lastSegment := pathSegments[len(pathSegments)-1]
		if strings.Contains(lastSegment, ".") {
			dotIndex := strings.LastIndex(lastSegment, ".")
			extension := lastSegment[dotIndex+1:]

			// Common file extensions that should be treated as files
			fileExtensions := map[string]bool{
				"html": true, "htm": true, "php": true, "asp": true, "aspx": true,
				"jsp": true, "js": true, "css": true, "xml": true, "json": true,
				"txt": true, "pdf": true, "doc": true, "docx": true, "jpg": true,
				"jpeg": true, "png": true, "gif": true, "svg": true, "ico": true,
				"zip": true, "tar": true, "gz": true, "mp3": true, "mp4": true,
				"woff": true, "woff2": true, "ttf": true, "otf": true, "eot": true,
			}

			if fileExtensions[strings.ToLower(extension)] {
				// It's a file with a recognized extension, use the parent directory
				newURL := *currentURL
				newURL.Path = strings.TrimSuffix(currentURL.Path, lastSegment)
				if !strings.HasSuffix(newURL.Path, "/") {
					newURL.Path += "/"
				}
				return &newURL
			}
		}
	}

	// If no file extension, try Content-Type detection for non-HTML types
	if isFile, err := c.isFileByContentType(currentURL.String()); err == nil {
		if isFile {
			// It's a file, use the parent directory
			newURL := *currentURL
			pathSegments := strings.Split(currentURL.Path, "/")
			if len(pathSegments) > 0 {
				// Remove the last segment (filename) and ensure trailing slash
				newURL.Path = strings.TrimSuffix(currentURL.Path, pathSegments[len(pathSegments)-1])
				if !strings.HasSuffix(newURL.Path, "/") {
					newURL.Path += "/"
				}
			}
			return &newURL
		} else {
			// It's not a file (likely a directory), add trailing slash
			newURL := *currentURL
			newURL.Path += "/"
			return &newURL
		}
	}

	// Fallback: URLs without file extensions are treated as directories
	newURL := *currentURL
	newURL.Path += "/"
	return &newURL
}

// isFileByContentType makes a HEAD request to determine if the URL represents a file
// based on the Content-Type header
func (c *Checker) isFileByContentType(urlStr string) (bool, error) {
	req, err := http.NewRequest("HEAD", urlStr, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("User-Agent", c.config.UserAgent)

	resp, err := c.client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	// If the request failed, we can't determine the type
	if resp.StatusCode >= 400 {
		return false, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		return false, fmt.Errorf("no Content-Type header")
	}

	// Parse the Content-Type to get just the MIME type (ignore charset, etc.)
	mimeType := strings.Split(contentType, ";")[0]
	mimeType = strings.TrimSpace(strings.ToLower(mimeType))

	// Determine if this MIME type represents a file vs directory
	return c.isFileMimeType(mimeType), nil
}

// isFileMimeType determines if a MIME type represents a file (vs a directory/page)
// This method is used in conjunction with URL path analysis to make the final determination
func (c *Checker) isFileMimeType(mimeType string) bool {
	// Directory-like MIME types (should be treated as directories)
	// These are typically API endpoints or directory listings
	directoryTypes := map[string]bool{
		"text/plain":       true, // Could be either, but often used for directory listings
		"application/json": true, // API endpoints should be treated as directories
		"application/xml":  true, // XML documents can contain relative links
		"text/xml":         true, // XML documents can contain relative links
	}

	// If it's explicitly a directory-like type, it's not a file
	if directoryTypes[mimeType] {
		return false
	}

	// HTML types need special handling - they could be either files or directories
	// depending on the URL structure. The caller will use URL path analysis to make the final decision.

	// File-like MIME types (should use parent directory for relative links)
	fileTypes := map[string]bool{
		// Documents
		"application/pdf":    true,
		"application/msword": true,
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document": true,
		"application/vnd.ms-excel": true,
		"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":         true,
		"application/vnd.ms-powerpoint":                                             true,
		"application/vnd.openxmlformats-officedocument.presentationml.presentation": true,
		"application/rtf": true,

		// Archives
		"application/zip":              true,
		"application/x-rar-compressed": true,
		"application/x-7z-compressed":  true,
		"application/x-tar":            true,
		"application/gzip":             true,
		"application/x-gzip":           true,

		// Images
		"image/jpeg":    true,
		"image/png":     true,
		"image/gif":     true,
		"image/webp":    true,
		"image/svg+xml": true,
		"image/bmp":     true,
		"image/tiff":    true,
		"image/x-icon":  true,

		// Audio
		"audio/mpeg": true,
		"audio/wav":  true,
		"audio/ogg":  true,
		"audio/mp4":  true,
		"audio/aac":  true,
		"audio/flac": true,

		// Video
		"video/mp4":       true,
		"video/mpeg":      true,
		"video/quicktime": true,
		"video/x-msvideo": true,
		"video/webm":      true,

		// Code/Text files that are typically static assets
		"application/javascript": true,
		"text/css":               true,
		"text/csv":               true,

		// Fonts
		"font/woff":              true,
		"font/woff2":             true,
		"application/font-woff":  true,
		"application/font-woff2": true,
		"font/ttf":               true,
		"font/otf":               true,

		// Other binary formats
		"application/octet-stream": true,
	}

	return fileTypes[mimeType]
}

// getResolveBaseURLByExtension is the fallback method using file extensions
// when HTTP Content-Type detection fails
func (c *Checker) getResolveBaseURLByExtension(currentURL *url.URL) *url.URL {
	// If the URL already ends with a slash, it's already a directory
	if strings.HasSuffix(currentURL.Path, "/") {
		return currentURL
	}

	// Check if the last path segment looks like a file (has an extension)
	pathSegments := strings.Split(currentURL.Path, "/")
	if len(pathSegments) > 0 {
		lastSegment := pathSegments[len(pathSegments)-1]

		// If it has a file extension (contains a dot and the extension is reasonable),
		// treat it as a file and use the parent directory
		if strings.Contains(lastSegment, ".") {
			dotIndex := strings.LastIndex(lastSegment, ".")
			extension := lastSegment[dotIndex+1:]

			// Common file extensions that should be treated as files
			fileExtensions := map[string]bool{
				"html": true, "htm": true, "php": true, "asp": true, "aspx": true,
				"jsp": true, "js": true, "css": true, "xml": true, "json": true,
				"txt": true, "pdf": true, "doc": true, "docx": true, "jpg": true,
				"jpeg": true, "png": true, "gif": true, "svg": true, "ico": true,
				"zip": true, "tar": true, "gz": true, "mp3": true, "mp4": true,
				"woff": true, "woff2": true, "ttf": true, "otf": true, "eot": true,
			}

			if fileExtensions[strings.ToLower(extension)] {
				// It's a file, use the parent directory
				newURL := *currentURL
				newURL.Path = strings.TrimSuffix(currentURL.Path, lastSegment)
				if !strings.HasSuffix(newURL.Path, "/") {
					newURL.Path += "/"
				}
				return &newURL
			}
		}
	}

	// No file extension or not a recognized file extension,
	// treat it as a directory by adding a trailing slash
	newURL := *currentURL
	newURL.Path += "/"
	return &newURL
}

// CheckLinks checks all provided URLs for broken links
func (c *Checker) CheckLinks(urls []string) []LinkResult {
	results := make([]LinkResult, len(urls))
	var wg sync.WaitGroup
	var mu sync.Mutex
	checked := 0

	// Use a semaphore to limit concurrent requests
	semaphore := make(chan struct{}, c.config.MaxConcurrent)

	for i, url := range urls {
		wg.Add(1)
		go func(index int, checkURL string) {
			defer wg.Done()

			// Acquire semaphore
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			// Rate limiting
			if err := c.limiter.Wait(context.Background()); err != nil {
				results[index] = LinkResult{
					URL:      checkURL,
					Error:    fmt.Sprintf("rate limiter error: %v", err),
					Duration: "0s",
				}
				return
			}

			result := c.checkSingleLink(checkURL)
			results[index] = result

			if c.config.Verbose {
				mu.Lock()
				checked++
				emoji := c.getStatusEmoji(result.StatusCode)
				fmt.Printf("%s [%d/%d] %s (Status: %d, Duration: %s)\n",
					emoji, checked, len(urls), result.URL, result.StatusCode, result.Duration)
				mu.Unlock()
			}
		}(i, url)
	}

	wg.Wait()
	return results
}

// checkSingleLink checks a single URL and returns the result
func (c *Checker) checkSingleLink(checkURL string) LinkResult {
	start := time.Now()

	req, err := http.NewRequest("HEAD", checkURL, nil)
	if err != nil {
		return LinkResult{
			URL:      checkURL,
			Error:    fmt.Sprintf("creating request: %v", err),
			Duration: time.Since(start).String(),
		}
	}
	req.Header.Set("User-Agent", c.config.UserAgent)

	resp, err := c.client.Do(req)
	if err != nil {
		// Try GET request if HEAD fails
		req.Method = "GET"
		resp, err = c.client.Do(req)
		if err != nil {
			return LinkResult{
				URL:      checkURL,
				Error:    fmt.Sprintf("request failed: %v", err),
				Duration: time.Since(start).String(),
			}
		}
	}
	defer resp.Body.Close()

	result := LinkResult{
		URL:        checkURL,
		StatusCode: resp.StatusCode,
		Duration:   time.Since(start).String(),
	}

	if resp.StatusCode >= 400 {
		result.Error = fmt.Sprintf("HTTP %d %s", resp.StatusCode, resp.Status)
	}

	return result
}

// shouldExclude checks if a URL should be excluded based on patterns
func (c *Checker) shouldExclude(url string) bool {
	for _, pattern := range c.config.ExcludePatterns {
		if pattern.MatchString(url) {
			return true
		}
	}
	return false
}

// getStatusEmoji returns an emoji based on HTTP status code
func (c *Checker) getStatusEmoji(statusCode int) string {
	switch {
	case statusCode == 0:
		return "❓" // Unknown/Error
	case statusCode >= 200 && statusCode < 300:
		return "✅" // Success
	case statusCode >= 300 && statusCode < 400:
		return "🔄" // Redirect
	case statusCode >= 400 && statusCode < 500:
		return "❌" // Client Error
	case statusCode >= 500:
		return "💥" // Server Error
	default:
		return "❓" // Unknown
	}
}
