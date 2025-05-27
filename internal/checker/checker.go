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

	"github.com/joshbeard/gh-action-link-checker/internal/config"
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
		mu.Unlock()

		if depth == maxDepth {
			return
		}

		links, err := c.extractLinksFromPage(currentURL, baseURLParsed)
		if err != nil {
			return
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
func (c *Checker) extractLinksFromPage(pageURL string, baseURL *url.URL) ([]string, error) {
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

	var links []string
	var extract func(*html.Node)
	extract = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			for _, attr := range n.Attr {
				if attr.Key == "href" {
					link := attr.Val
					if absoluteURL := c.resolveURL(link, baseURL); absoluteURL != "" {
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
			c.limiter.Wait(context.Background())

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
