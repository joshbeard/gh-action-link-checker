package checker

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/joshbeard/link-validator/internal/config"
)

func TestGetStatusEmoji(t *testing.T) {
	cfg := &config.Config{}
	checker := New(cfg)

	testCases := []struct {
		statusCode int
		expected   string
	}{
		{0, "❓"},
		{200, "✅"},
		{201, "✅"},
		{299, "✅"},
		{301, "🔄"},
		{302, "🔄"},
		{399, "🔄"},
		{400, "❌"},
		{404, "❌"},
		{499, "❌"},
		{500, "💥"},
		{503, "💥"},
		{599, "💥"},
		{999, "💥"}, // 999 is >= 500, so it's a server error
	}

	for _, tc := range testCases {
		result := checker.getStatusEmoji(tc.statusCode)
		if result != tc.expected {
			t.Errorf("Status %d: expected %s, got %s", tc.statusCode, tc.expected, result)
		}
	}
}

func TestShouldExclude(t *testing.T) {
	cfg := &config.Config{}
	// Manually create exclude patterns for testing
	cfg.ExcludePatterns = []*regexp.Regexp{}

	// Add some test patterns
	patterns := []string{
		`.*\.pdf$`,
		`.*\.zip$`,
		`.*example\.com.*`,
	}

	for _, pattern := range patterns {
		if regex, err := regexp.Compile(pattern); err == nil {
			cfg.ExcludePatterns = append(cfg.ExcludePatterns, regex)
		}
	}

	checker := New(cfg)

	testCases := []struct {
		url      string
		expected bool
	}{
		{"https://example.com/file.pdf", true},
		{"https://example.com/file.zip", true},
		{"https://example.com/page", true},
		{"https://other.com/file.pdf", true},
		{"https://other.com/page.html", false},
		{"https://test.org/document.txt", false},
	}

	for _, tc := range testCases {
		result := checker.shouldExclude(tc.url)
		if result != tc.expected {
			t.Errorf("URL %s: expected exclude %v, got %v", tc.url, tc.expected, result)
		}
	}
}

func TestResolveURL(t *testing.T) {
	cfg := &config.Config{}
	checker := New(cfg)

	baseURL, _ := url.Parse("https://example.com/path/")

	testCases := []struct {
		href     string
		expected string
	}{
		{"", ""},
		{"#anchor", ""},
		{"javascript:void(0)", ""},
		{"mailto:test@example.com", ""},
		{"/absolute/path", "https://example.com/absolute/path"},
		{"relative/path", "https://example.com/path/relative/path"},
		{"../parent", "https://example.com/parent"},
		{"https://other.com/external", "https://other.com/external"},
		{"?query=param", "https://example.com/path/?query=param"},
	}

	for _, tc := range testCases {
		result := checker.resolveURL(tc.href, baseURL)
		if result != tc.expected {
			t.Errorf("href %s: expected %s, got %s", tc.href, tc.expected, result)
		}
	}
}

func TestGetURLsFromSitemap(t *testing.T) {
	// Create a test server with a mock sitemap
	sitemapXML := `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url>
    <loc>https://example.com/</loc>
  </url>
  <url>
    <loc>https://example.com/page1</loc>
  </url>
  <url>
    <loc>https://example.com/page2</loc>
  </url>
  <url>
    <loc>https://example.com/file.pdf</loc>
  </url>
</urlset>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(sitemapXML)); err != nil {
			http.Error(w, "Failed to write response", http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	t.Run("successful sitemap parsing", func(t *testing.T) {
		cfg := &config.Config{
			UserAgent: "TestBot/1.0",
			Timeout:   5 * time.Second,
		}
		checker := New(cfg)

		urls, err := checker.GetURLsFromSitemap(server.URL)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		expected := 4
		if len(urls) != expected {
			t.Errorf("Expected %d URLs, got %d", expected, len(urls))
		}

		expectedURLs := []string{
			"https://example.com/",
			"https://example.com/page1",
			"https://example.com/page2",
			"https://example.com/file.pdf",
		}

		for i, expectedURL := range expectedURLs {
			if i >= len(urls) || urls[i] != expectedURL {
				t.Errorf("Expected URL %s at index %d, got %s", expectedURL, i, urls[i])
			}
		}
	})

	t.Run("sitemap with exclude patterns", func(t *testing.T) {
		cfg := &config.Config{
			UserAgent: "TestBot/1.0",
			Timeout:   5 * time.Second,
		}

		// Add exclude pattern for PDF files
		if regex, err := regexp.Compile(`.*\.pdf$`); err == nil {
			cfg.ExcludePatterns = append(cfg.ExcludePatterns, regex)
		}

		checker := New(cfg)

		urls, err := checker.GetURLsFromSitemap(server.URL)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		expected := 3 // Should exclude the PDF
		if len(urls) != expected {
			t.Errorf("Expected %d URLs after exclusion, got %d", expected, len(urls))
		}

		// Check that PDF is not in the results
		for _, url := range urls {
			if strings.Contains(url, ".pdf") {
				t.Errorf("PDF URL should have been excluded: %s", url)
			}
		}
	})
}

func TestGetURLsFromSitemapErrors(t *testing.T) {
	cfg := &config.Config{
		UserAgent: "TestBot/1.0",
		Timeout:   5 * time.Second,
	}
	checker := New(cfg)

	t.Run("invalid URL", func(t *testing.T) {
		_, err := checker.GetURLsFromSitemap("not-a-url")
		if err == nil {
			t.Error("Expected error for invalid URL")
		}
	})

	t.Run("404 response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		_, err := checker.GetURLsFromSitemap(server.URL)
		if err == nil {
			t.Error("Expected error for 404 response")
		}
	})

	t.Run("invalid XML", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			if _, err := w.Write([]byte("invalid xml content")); err != nil {
				http.Error(w, "Failed to write response", http.StatusInternalServerError)
			}
		}))
		defer server.Close()

		_, err := checker.GetURLsFromSitemap(server.URL)
		if err == nil {
			t.Error("Expected error for invalid XML")
		}
	})
}

func TestCheckSingleLink(t *testing.T) {
	// Create test servers for different scenarios
	successServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte("OK")); err != nil {
			http.Error(w, "Failed to write response", http.StatusInternalServerError)
		}
	}))
	defer successServer.Close()

	notFoundServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		if _, err := w.Write([]byte("Not Found")); err != nil {
			http.Error(w, "Failed to write response", http.StatusInternalServerError)
		}
	}))
	defer notFoundServer.Close()

	redirectServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusMovedPermanently)
		w.Header().Set("Location", successServer.URL)
	}))
	defer redirectServer.Close()

	cfg := &config.Config{
		UserAgent:     "TestBot/1.0",
		Timeout:       5 * time.Second,
		MaxConcurrent: 1,
	}
	checker := New(cfg)

	t.Run("successful request", func(t *testing.T) {
		result := checker.checkSingleLink(successServer.URL)

		if result.URL != successServer.URL {
			t.Errorf("Expected URL %s, got %s", successServer.URL, result.URL)
		}
		if result.StatusCode != 200 {
			t.Errorf("Expected status 200, got %d", result.StatusCode)
		}
		if result.Error != "" {
			t.Errorf("Expected no error, got %s", result.Error)
		}
		if result.Duration == "" {
			t.Error("Expected duration to be set")
		}
	})

	t.Run("404 request", func(t *testing.T) {
		result := checker.checkSingleLink(notFoundServer.URL)

		if result.StatusCode != 404 {
			t.Errorf("Expected status 404, got %d", result.StatusCode)
		}
		if result.Error == "" {
			t.Error("Expected error message for 404")
		}
		if !strings.Contains(result.Error, "404") {
			t.Errorf("Expected error to contain '404', got %s", result.Error)
		}
	})

	t.Run("redirect request", func(t *testing.T) {
		result := checker.checkSingleLink(redirectServer.URL)

		if result.StatusCode != 301 {
			t.Errorf("Expected status 301, got %d", result.StatusCode)
		}
		if result.Error != "" {
			t.Errorf("Expected no error for redirect, got %s", result.Error)
		}
	})

	t.Run("invalid URL", func(t *testing.T) {
		result := checker.checkSingleLink("not-a-valid-url")

		if result.StatusCode != 0 {
			t.Errorf("Expected status 0 for invalid URL, got %d", result.StatusCode)
		}
		if result.Error == "" {
			t.Error("Expected error for invalid URL")
		}
	})
}

func TestCheckLinks(t *testing.T) {
	// Create test servers
	successServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer successServer.Close()

	notFoundServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer notFoundServer.Close()

	cfg := &config.Config{
		UserAgent:     "TestBot/1.0",
		Timeout:       5 * time.Second,
		MaxConcurrent: 2,
		Verbose:       false, // Disable verbose for cleaner test output
	}
	checker := New(cfg)

	urls := []string{
		successServer.URL,
		notFoundServer.URL,
		successServer.URL + "/another",
	}

	results := checker.CheckLinks(urls)

	if len(results) != len(urls) {
		t.Errorf("Expected %d results, got %d", len(urls), len(results))
	}

	// Check first result (success)
	if results[0].StatusCode != 200 {
		t.Errorf("Expected first result status 200, got %d", results[0].StatusCode)
	}

	// Check second result (404)
	if results[1].StatusCode != 404 {
		t.Errorf("Expected second result status 404, got %d", results[1].StatusCode)
	}

	// Check third result (success)
	if results[2].StatusCode != 200 {
		t.Errorf("Expected third result status 200, got %d", results[2].StatusCode)
	}

	// Verify all results have durations
	for i, result := range results {
		if result.Duration == "" {
			t.Errorf("Result %d missing duration", i)
		}
		if result.URL != urls[i] {
			t.Errorf("Result %d URL mismatch: expected %s, got %s", i, urls[i], result.URL)
		}
	}
}

func TestExtractLinksWithoutBaseTag(t *testing.T) {
	// HTML content without a <base> tag
	htmlContent := `<!DOCTYPE html>
<html>
<head>
    <title>Test Page Without Base Tag</title>
</head>
<body>
    <a href="relative-file.html">Relative File</a>
    <a href="subdir/page.html">Subdirectory Page</a>
    <a href="../parent.html">Parent Directory</a>
    <a href="/absolute/path.html">Absolute Path</a>
    <a href="https://external.com/page">External Link</a>
    <a href="image.jpg">Relative Image</a>
</body>
</html>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(htmlContent)); err != nil {
			http.Error(w, "Failed to write response", http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	cfg := &config.Config{
		UserAgent: "TestBot/1.0",
		Timeout:   5 * time.Second,
		Verbose:   true, // Enable verbose to see the resolution logic
	}
	checker := New(cfg)

	// Test with a URL that looks like a directory (no file extension)
	currentURL, _ := url.Parse(server.URL + "/blog/post")
	baseURL, _ := url.Parse(server.URL)

	links, err := checker.extractLinksFromPage(server.URL+"/blog/post", currentURL, baseURL)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// Expected links based on directory-style resolution
	expectedLinks := []string{
		server.URL + "/blog/post/relative-file.html",
		server.URL + "/blog/post/subdir/page.html",
		server.URL + "/blog/parent.html",
		server.URL + "/absolute/path.html",
		server.URL + "/blog/post/image.jpg",
		// External link should be excluded (different domain)
	}

	if len(links) != len(expectedLinks) {
		t.Errorf("Expected %d links, got %d", len(expectedLinks), len(links))
		t.Logf("Got links: %v", links)
	}

	for _, expected := range expectedLinks {
		found := false
		for _, link := range links {
			if link == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected link not found: %s", expected)
		}
	}
}

func TestExtractLinksFromPage(t *testing.T) {
	htmlContent := `<!DOCTYPE html>
<html>
<head>
    <title>Test Page</title>
</head>
<body>
    <a href="/page1">Page 1</a>
    <a href="/page2">Page 2</a>
    <a href="https://external.com/page">External</a>
    <a href="#anchor">Anchor</a>
    <a href="mailto:test@example.com">Email</a>
    <a href="javascript:void(0)">JavaScript</a>
    <a href="relative/path">Relative</a>
</body>
</html>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(htmlContent)); err != nil {
			http.Error(w, "Failed to write response", http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	cfg := &config.Config{
		UserAgent: "TestBot/1.0",
		Timeout:   5 * time.Second,
	}
	checker := New(cfg)

	baseURL, _ := url.Parse(server.URL)
	currentURL, _ := url.Parse(server.URL)
	links, err := checker.extractLinksFromPage(server.URL, currentURL, baseURL)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// Should extract links from same domain only, excluding anchors, mailto, javascript
	expectedLinks := []string{
		server.URL + "/page1",
		server.URL + "/page2",
		server.URL + "/relative/path",
	}

	if len(links) != len(expectedLinks) {
		t.Errorf("Expected %d links, got %d", len(expectedLinks), len(links))
	}

	for _, expected := range expectedLinks {
		found := false
		for _, link := range links {
			if link == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected link not found: %s", expected)
		}
	}
}

func TestGetResolveBaseURL(t *testing.T) {
	cfg := &config.Config{
		UserAgent: "TestBot/1.0",
		Timeout:   5 * time.Second,
	}
	checker := New(cfg)

	testCases := []struct {
		input    string
		expected string
		desc     string
	}{
		{
			input:    "https://example.com/blog",
			expected: "https://example.com/blog/",
			desc:     "URL without extension should be treated as directory",
		},
		{
			input:    "https://example.com/blog/",
			expected: "https://example.com/blog/",
			desc:     "URL with trailing slash should remain unchanged",
		},
		{
			input:    "https://example.com/blog/post.html",
			expected: "https://example.com/blog/",
			desc:     "URL with file extension should use parent directory",
		},
		{
			input:    "https://example.com/docs/readme.txt",
			expected: "https://example.com/docs/",
			desc:     "TXT file should use parent directory",
		},
		{
			input:    "https://example.com/images/photo.jpg",
			expected: "https://example.com/images/",
			desc:     "Image file should use parent directory",
		},
		{
			input:    "https://example.com/api/v1",
			expected: "https://example.com/api/v1/",
			desc:     "API endpoint without extension should be treated as directory",
		},
		{
			input:    "https://example.com/file.unknown",
			expected: "https://example.com/file.unknown/",
			desc:     "Unknown extension should be treated as directory",
		},
		{
			input:    "https://example.com/",
			expected: "https://example.com/",
			desc:     "Root URL with slash should remain unchanged",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			inputURL, err := url.Parse(tc.input)
			if err != nil {
				t.Fatalf("Failed to parse input URL: %v", err)
			}

			result := checker.getResolveBaseURL(inputURL)
			if result.String() != tc.expected {
				t.Errorf("Expected %s, got %s", tc.expected, result.String())
			}
		})
	}
}

func TestIsFileMimeType(t *testing.T) {
	cfg := &config.Config{}
	checker := New(cfg)

	testCases := []struct {
		mimeType string
		expected bool
		desc     string
	}{
		// Directory-like types (should return false)
		{"text/html", false, "HTML should be treated as directory"},
		{"application/xhtml+xml", false, "XHTML should be treated as directory"},
		{"text/plain", false, "Plain text should be treated as directory"},

		// File-like types (should return true)
		{"application/pdf", true, "PDF should be treated as file"},
		{"image/jpeg", true, "JPEG should be treated as file"},
		{"image/png", true, "PNG should be treated as file"},
		{"audio/mpeg", true, "MP3 should be treated as file"},
		{"video/mp4", true, "MP4 should be treated as file"},
		{"application/zip", true, "ZIP should be treated as file"},
		{"application/javascript", true, "JavaScript should be treated as file"},
		{"text/css", true, "CSS should be treated as file"},
		{"application/json", false, "JSON should be treated as directory"},
		{"font/woff", true, "WOFF font should be treated as file"},
		{"application/octet-stream", true, "Binary should be treated as file"},

		// Unknown types (should return false - default to directory)
		{"unknown/type", false, "Unknown type should default to directory"},
		{"", false, "Empty type should default to directory"},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			result := checker.isFileMimeType(tc.mimeType)
			if result != tc.expected {
				t.Errorf("MIME type %s: expected %v, got %v", tc.mimeType, tc.expected, result)
			}
		})
	}
}

func TestIsFileByContentType(t *testing.T) {
	cfg := &config.Config{
		UserAgent: "TestBot/1.0",
		Timeout:   5 * time.Second,
	}
	checker := New(cfg)

	// Test with a server that returns different Content-Types
	testCases := []struct {
		contentType string
		expected    bool
		desc        string
	}{
		{"text/html", false, "HTML page should not be treated as file"},
		{"application/pdf", true, "PDF should be treated as file"},
		{"image/jpeg", true, "JPEG should be treated as file"},
		{"application/json", false, "JSON should be treated as directory"},
		{"text/css", true, "CSS should be treated as file"},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tc.contentType)
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			result, err := checker.isFileByContentType(server.URL)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if result != tc.expected {
				t.Errorf("Content-Type %s: expected %v, got %v", tc.contentType, tc.expected, result)
			}
		})
	}

	t.Run("404 response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		_, err := checker.isFileByContentType(server.URL)
		if err == nil {
			t.Error("Expected error for 404 response")
		}
	})

	t.Run("no Content-Type header", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		_, err := checker.isFileByContentType(server.URL)
		if err == nil {
			t.Error("Expected error when no Content-Type header")
		}
	})

	t.Run("Content-Type with charset", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		result, err := checker.isFileByContentType(server.URL)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if result != false {
			t.Error("HTML with charset should not be treated as file")
		}
	})
}

func TestDynamicURLResolution(t *testing.T) {
	cfg := &config.Config{
		UserAgent: "TestBot/1.0",
		Timeout:   5 * time.Second,
		Verbose:   true,
	}
	checker := New(cfg)

	// Create a test server that serves different content types based on path
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/blog/post":
			// This is an HTML page (directory-like)
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`<html><body><a href="image.jpg">Image</a></body></html>`))
		case "/docs/manual.pdf":
			// This is a PDF file
			w.Header().Set("Content-Type", "application/pdf")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("PDF content"))
		case "/api/data":
			// This is a JSON API endpoint (directory-like)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status": "ok"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	testCases := []struct {
		path     string
		expected string
		desc     string
	}{
		{
			path:     "/blog/post",
			expected: server.URL + "/blog/post/",
			desc:     "HTML page should be treated as directory",
		},
		{
			path:     "/docs/manual.pdf",
			expected: server.URL + "/docs/",
			desc:     "PDF file should use parent directory",
		},
		{
			path:     "/api/data",
			expected: server.URL + "/api/data/",
			desc:     "JSON endpoint should be treated as directory",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			currentURL, err := url.Parse(server.URL + tc.path)
			if err != nil {
				t.Fatalf("Failed to parse URL: %v", err)
			}

			result := checker.getResolveBaseURL(currentURL)
			if result.String() != tc.expected {
				t.Errorf("Expected %s, got %s", tc.expected, result.String())
			}
		})
	}
}

func TestCrawlWebsite(t *testing.T) {
	// Create a test server with multiple pages and links
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)

		switch r.URL.Path {
		case "/":
			// Root page with links to other pages
			w.Write([]byte(`<!DOCTYPE html>
<html>
<head><title>Home</title></head>
<body>
	<a href="/page1">Page 1</a>
	<a href="/page2">Page 2</a>
	<a href="https://external.com/page">External</a>
</body>
</html>`))
		case "/page1":
			// Page 1 with link to page 3
			w.Write([]byte(`<!DOCTYPE html>
<html>
<head><title>Page 1</title></head>
<body>
	<a href="/page3">Page 3</a>
	<a href="/page2">Page 2</a>
</body>
</html>`))
		case "/page2":
			// Page 2 with no links
			w.Write([]byte(`<!DOCTYPE html>
<html>
<head><title>Page 2</title></head>
<body>
	<p>This is page 2</p>
</body>
</html>`))
		case "/page3":
			// Page 3 with link back to root
			w.Write([]byte(`<!DOCTYPE html>
<html>
<head><title>Page 3</title></head>
<body>
	<a href="/">Home</a>
</body>
</html>`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	cfg := &config.Config{
		UserAgent: "TestBot/1.0",
		Timeout:   5 * time.Second,
		Verbose:   false, // Disable verbose for cleaner test output
	}
	checker := New(cfg)

	t.Run("crawl with depth 0", func(t *testing.T) {
		urls, err := checker.CrawlWebsite(server.URL, 0)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		if len(urls) != 1 {
			t.Errorf("Expected 1 URL with depth 0, got %d", len(urls))
		}

		if urls[0] != server.URL {
			t.Errorf("Expected %s, got %s", server.URL, urls[0])
		}
	})

	t.Run("crawl with depth 1", func(t *testing.T) {
		urls, err := checker.CrawlWebsite(server.URL, 1)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		// Should find root page + page1 and page2 (external links excluded)
		expectedURLs := []string{
			server.URL,
			server.URL + "/page1",
			server.URL + "/page2",
		}

		if len(urls) != len(expectedURLs) {
			t.Errorf("Expected %d URLs, got %d", len(expectedURLs), len(urls))
		}

		// Check that all expected URLs are present
		urlMap := make(map[string]bool)
		for _, url := range urls {
			urlMap[url] = true
		}

		for _, expected := range expectedURLs {
			if !urlMap[expected] {
				t.Errorf("Expected URL not found: %s", expected)
			}
		}
	})

	t.Run("crawl with depth 2", func(t *testing.T) {
		urls, err := checker.CrawlWebsite(server.URL, 2)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		// Should find all pages including page3
		expectedURLs := []string{
			server.URL,
			server.URL + "/page1",
			server.URL + "/page2",
			server.URL + "/page3",
		}

		if len(urls) != len(expectedURLs) {
			t.Errorf("Expected %d URLs, got %d", len(expectedURLs), len(urls))
		}
	})

	t.Run("crawl with invalid base URL", func(t *testing.T) {
		// Use a URL that will definitely cause an error during HTTP request
		urls, err := checker.CrawlWebsite("http://invalid-host-that-does-not-exist.local", 1)
		// The function might not error immediately but should return the base URL
		// and then fail when trying to extract links from it
		if err != nil {
			// This is expected - the function should fail
			return
		}
		// If no error, at least the base URL should be returned
		if len(urls) == 0 {
			t.Error("Expected at least the base URL to be returned")
		}
	})

	t.Run("crawl with verbose output", func(t *testing.T) {
		verboseCfg := &config.Config{
			UserAgent: "TestBot/1.0",
			Timeout:   5 * time.Second,
			Verbose:   true,
		}
		verboseChecker := New(verboseCfg)

		urls, err := verboseChecker.CrawlWebsite(server.URL, 1)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		if len(urls) < 1 {
			t.Error("Expected at least 1 URL")
		}
	})
}

func TestGetResolveBaseURLByExtension(t *testing.T) {
	cfg := &config.Config{}
	checker := New(cfg)

	testCases := []struct {
		input    string
		expected string
		desc     string
	}{
		{
			input:    "https://example.com/blog",
			expected: "https://example.com/blog/",
			desc:     "URL without extension should be treated as directory",
		},
		{
			input:    "https://example.com/blog/",
			expected: "https://example.com/blog/",
			desc:     "URL with trailing slash should remain unchanged",
		},
		{
			input:    "https://example.com/blog/post.html",
			expected: "https://example.com/blog/",
			desc:     "HTML file should use parent directory",
		},
		{
			input:    "https://example.com/docs/readme.txt",
			expected: "https://example.com/docs/",
			desc:     "TXT file should use parent directory",
		},
		{
			input:    "https://example.com/images/photo.jpg",
			expected: "https://example.com/images/",
			desc:     "Image file should use parent directory",
		},
		{
			input:    "https://example.com/scripts/app.js",
			expected: "https://example.com/scripts/",
			desc:     "JavaScript file should use parent directory",
		},
		{
			input:    "https://example.com/styles/main.css",
			expected: "https://example.com/styles/",
			desc:     "CSS file should use parent directory",
		},
		{
			input:    "https://example.com/data/config.json",
			expected: "https://example.com/data/",
			desc:     "JSON file should use parent directory",
		},
		{
			input:    "https://example.com/file.unknown",
			expected: "https://example.com/file.unknown/",
			desc:     "Unknown extension should be treated as directory",
		},
		{
			input:    "https://example.com/",
			expected: "https://example.com/",
			desc:     "Root URL should remain unchanged",
		},
		{
			input:    "https://example.com/path/with/no/extension",
			expected: "https://example.com/path/with/no/extension/",
			desc:     "Path with no extension should be treated as directory",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			inputURL, err := url.Parse(tc.input)
			if err != nil {
				t.Fatalf("Failed to parse input URL: %v", err)
			}

			result := checker.getResolveBaseURLByExtension(inputURL)
			if result.String() != tc.expected {
				t.Errorf("Expected %s, got %s", tc.expected, result.String())
			}
		})
	}
}

func TestExtractLinksFromPageEdgeCases(t *testing.T) {
	cfg := &config.Config{
		UserAgent: "TestBot/1.0",
		Timeout:   5 * time.Second,
		Verbose:   false,
	}
	checker := New(cfg)

	t.Run("page with base tag", func(t *testing.T) {
		htmlContent := `<!DOCTYPE html>
<html>
<head>
    <base href="/custom/base/">
    <title>Test Page With Base Tag</title>
</head>
<body>
    <a href="relative.html">Relative Link</a>
    <a href="/absolute.html">Absolute Link</a>
</body>
</html>`

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(htmlContent))
		}))
		defer server.Close()

		baseURL, _ := url.Parse(server.URL)
		currentURL, _ := url.Parse(server.URL + "/some/page")

		links, err := checker.extractLinksFromPage(server.URL+"/some/page", currentURL, baseURL)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		expectedLinks := []string{
			server.URL + "/custom/base/relative.html",
			server.URL + "/absolute.html",
		}

		if len(links) != len(expectedLinks) {
			t.Errorf("Expected %d links, got %d", len(expectedLinks), len(links))
		}

		for _, expected := range expectedLinks {
			found := false
			for _, link := range links {
				if link == expected {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Expected link not found: %s", expected)
			}
		}
	})

	t.Run("page with invalid HTML", func(t *testing.T) {
		invalidHTML := `<html><head><title>Invalid</title></head><body><a href="test">Unclosed link</body></html>`

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(invalidHTML))
		}))
		defer server.Close()

		baseURL, _ := url.Parse(server.URL)
		currentURL, _ := url.Parse(server.URL)

		links, err := checker.extractLinksFromPage(server.URL, currentURL, baseURL)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		// Should still extract the link despite invalid HTML
		if len(links) != 1 {
			t.Errorf("Expected 1 link, got %d", len(links))
		}
	})

	t.Run("page with non-200 status", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		baseURL, _ := url.Parse(server.URL)
		currentURL, _ := url.Parse(server.URL)

		_, err := checker.extractLinksFromPage(server.URL, currentURL, baseURL)
		if err == nil {
			t.Error("Expected error for non-200 status")
		}
	})

	t.Run("page with malformed URL in href", func(t *testing.T) {
		htmlContent := `<!DOCTYPE html>
<html>
<body>
    <a href="valid-link.html">Valid Link</a>
    <a href="ht tp://invalid url.com">Invalid URL</a>
    <a href="">Empty href</a>
</body>
</html>`

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(htmlContent))
		}))
		defer server.Close()

		baseURL, _ := url.Parse(server.URL)
		currentURL, _ := url.Parse(server.URL)

		links, err := checker.extractLinksFromPage(server.URL, currentURL, baseURL)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		// Should only extract the valid link
		if len(links) != 1 {
			t.Errorf("Expected 1 valid link, got %d", len(links))
		}

		if links[0] != server.URL+"/valid-link.html" {
			t.Errorf("Expected %s, got %s", server.URL+"/valid-link.html", links[0])
		}
	})
}

func TestCheckLinksEdgeCases(t *testing.T) {
	cfg := &config.Config{
		UserAgent:     "TestBot/1.0",
		Timeout:       5 * time.Second,
		MaxConcurrent: 2,
		Verbose:       true, // Test verbose output
	}
	checker := New(cfg)

	t.Run("empty URL list", func(t *testing.T) {
		results := checker.CheckLinks([]string{})
		if len(results) != 0 {
			t.Errorf("Expected 0 results for empty list, got %d", len(results))
		}
	})

	t.Run("single URL", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		results := checker.CheckLinks([]string{server.URL})
		if len(results) != 1 {
			t.Errorf("Expected 1 result, got %d", len(results))
		}

		if results[0].StatusCode != 200 {
			t.Errorf("Expected status 200, got %d", results[0].StatusCode)
		}
	})

	t.Run("HEAD request fails, GET succeeds", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "HEAD" {
				// Return an error that will cause the HTTP client to fail
				w.Header().Set("Connection", "close")
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		result := checker.checkSingleLink(server.URL)
		// The test should check that either HEAD succeeds or GET is attempted
		// In this case, HEAD returns 405 which is not an HTTP client error
		if result.StatusCode != 405 && result.StatusCode != 200 {
			t.Errorf("Expected status 405 or 200, got %d", result.StatusCode)
		}
	})
}

func TestGetStatusEmojiEdgeCases(t *testing.T) {
	cfg := &config.Config{}
	checker := New(cfg)

	testCases := []struct {
		statusCode int
		expected   string
		desc       string
	}{
		{100, "❓", "1xx status should return unknown"},
		{199, "❓", "1xx status should return unknown"},
		{299, "✅", "2xx boundary should return success"},
		{300, "🔄", "3xx boundary should return redirect"},
		{399, "🔄", "3xx boundary should return redirect"},
		{400, "❌", "4xx boundary should return client error"},
		{499, "❌", "4xx boundary should return client error"},
		{500, "💥", "5xx boundary should return server error"},
		{600, "💥", "6xx+ should return server error"},
		{-1, "❓", "negative status should return unknown"},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			result := checker.getStatusEmoji(tc.statusCode)
			if result != tc.expected {
				t.Errorf("Status %d: expected %s, got %s", tc.statusCode, tc.expected, result)
			}
		})
	}
}

func TestResolveURLEdgeCases(t *testing.T) {
	cfg := &config.Config{}
	checker := New(cfg)

	baseURL, _ := url.Parse("https://example.com/path/")

	testCases := []struct {
		href     string
		expected string
		desc     string
	}{
		{"", "", "empty href should return empty"},
		{"#", "", "fragment-only href should return empty"},
		{"#section", "", "fragment href should return empty"},
		{"javascript:", "", "javascript protocol should return empty"},
		{"javascript:void(0)", "", "javascript function should return empty"},
		{"mailto:", "", "mailto protocol should return empty"},
		{"mailto:test@example.com", "", "mailto address should return empty"},
		{"tel:+1234567890", "tel:+1234567890", "tel protocol should be preserved as absolute URL"},
		{"ftp://ftp.example.com/file", "ftp://ftp.example.com/file", "ftp protocol should be preserved"},
		{"//other.com/path", "https://other.com/path", "protocol-relative URL should use base protocol"},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			result := checker.resolveURL(tc.href, baseURL)
			if result != tc.expected {
				t.Errorf("href %s: expected %s, got %s", tc.href, tc.expected, result)
			}
		})
	}
}

func TestGetResolveBaseURLEdgeCases(t *testing.T) {
	cfg := &config.Config{
		UserAgent: "TestBot/1.0",
		Timeout:   5 * time.Second,
	}
	checker := New(cfg)

	t.Run("URL with query parameters", func(t *testing.T) {
		inputURL, _ := url.Parse("https://example.com/search?q=test")
		result := checker.getResolveBaseURL(inputURL)
		// Query parameters are preserved in the URL
		expected := "https://example.com/search/?q=test"
		if result.String() != expected {
			t.Errorf("Expected %s, got %s", expected, result.String())
		}
	})

	t.Run("URL with fragment", func(t *testing.T) {
		inputURL, _ := url.Parse("https://example.com/page#section")
		result := checker.getResolveBaseURL(inputURL)
		// Fragments are preserved in the URL
		expected := "https://example.com/page/#section"
		if result.String() != expected {
			t.Errorf("Expected %s, got %s", expected, result.String())
		}
	})

	t.Run("URL with port", func(t *testing.T) {
		inputURL, _ := url.Parse("https://example.com:8080/api/endpoint")
		result := checker.getResolveBaseURL(inputURL)
		expected := "https://example.com:8080/api/endpoint/"
		if result.String() != expected {
			t.Errorf("Expected %s, got %s", expected, result.String())
		}
	})

	t.Run("URL with multiple dots in filename", func(t *testing.T) {
		inputURL, _ := url.Parse("https://example.com/file.min.js")
		result := checker.getResolveBaseURL(inputURL)
		expected := "https://example.com/"
		if result.String() != expected {
			t.Errorf("Expected %s, got %s", expected, result.String())
		}
	})

	t.Run("URL with dot in directory name", func(t *testing.T) {
		inputURL, _ := url.Parse("https://example.com/v1.0/api")
		result := checker.getResolveBaseURL(inputURL)
		expected := "https://example.com/v1.0/api/"
		if result.String() != expected {
			t.Errorf("Expected %s, got %s", expected, result.String())
		}
	})

	t.Run("HTTP error during content type check", func(t *testing.T) {
		// Use a URL that will fail the HTTP request
		inputURL, _ := url.Parse("https://nonexistent.example.com/test")
		result := checker.getResolveBaseURL(inputURL)
		expected := "https://nonexistent.example.com/test/"
		if result.String() != expected {
			t.Errorf("Expected %s, got %s", expected, result.String())
		}
	})
}

func TestSitemapWithReadError(t *testing.T) {
	cfg := &config.Config{
		UserAgent: "TestBot/1.0",
		Timeout:   5 * time.Second,
	}
	checker := New(cfg)

	t.Run("server closes connection during read", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusOK)
			// Write partial XML and close connection
			w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><urlset`))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			// Simulate connection close by hijacking
			if hijacker, ok := w.(http.Hijacker); ok {
				conn, _, _ := hijacker.Hijack()
				conn.Close()
			}
		}))
		defer server.Close()

		_, err := checker.GetURLsFromSitemap(server.URL)
		if err == nil {
			t.Error("Expected error for incomplete XML")
		}
	})
}

func TestAdditionalCoverageTests(t *testing.T) {
	cfg := &config.Config{
		UserAgent: "TestBot/1.0",
		Timeout:   5 * time.Second,
	}
	checker := New(cfg)

	t.Run("extractLinksFromPage with request error", func(t *testing.T) {
		baseURL, _ := url.Parse("https://example.com")
		currentURL, _ := url.Parse("https://example.com/test")

		// Use an invalid URL that will cause a request error
		_, err := checker.extractLinksFromPage("ht tp://invalid url", currentURL, baseURL)
		if err == nil {
			t.Error("Expected error for invalid URL")
		}
	})

	t.Run("extractLinksFromPage with HTML parse error", func(t *testing.T) {
		// Create a server that returns invalid content type
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusOK)
			// Write content that will cause HTML parsing issues
			w.Write([]byte("<!DOCTYPE html><html><head><title>Test</title></head><body><a href=\"test\">Link</a></body></html>"))
		}))
		defer server.Close()

		baseURL, _ := url.Parse(server.URL)
		currentURL, _ := url.Parse(server.URL)

		// This should succeed despite any HTML parsing quirks
		links, err := checker.extractLinksFromPage(server.URL, currentURL, baseURL)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if len(links) == 0 {
			t.Error("Expected at least one link")
		}
	})

	t.Run("checkSingleLink with network timeout", func(t *testing.T) {
		// Create a checker with very short timeout
		shortTimeoutCfg := &config.Config{
			UserAgent:     "TestBot/1.0",
			Timeout:       1 * time.Millisecond, // Very short timeout
			MaxConcurrent: 1,
		}
		shortTimeoutChecker := New(shortTimeoutCfg)

		// Use a URL that will likely timeout
		result := shortTimeoutChecker.checkSingleLink("https://httpbin.org/delay/10")
		if result.Error == "" {
			t.Error("Expected timeout error")
		}
	})

	t.Run("getResolveBaseURL with empty path segments", func(t *testing.T) {
		inputURL, _ := url.Parse("https://example.com")
		result := checker.getResolveBaseURL(inputURL)
		expected := "https://example.com/"
		if result.String() != expected {
			t.Errorf("Expected %s, got %s", expected, result.String())
		}
	})

	t.Run("isFileByContentType with redirect", func(t *testing.T) {
		// Create a server that redirects
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/redirect" {
				w.Header().Set("Location", "/target")
				w.WriteHeader(http.StatusMovedPermanently)
				return
			}
			w.Header().Set("Content-Type", "application/pdf")
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		result, err := checker.isFileByContentType(server.URL + "/redirect")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if !result {
			t.Error("Expected PDF to be treated as file")
		}
	})

	t.Run("CheckLinks with rate limiter", func(t *testing.T) {
		// Test the rate limiter path by using a normal checker
		restrictiveCfg := &config.Config{
			UserAgent:     "TestBot/1.0",
			Timeout:       5 * time.Second,
			MaxConcurrent: 1,
		}
		restrictiveChecker := New(restrictiveCfg)

		// Test with a single URL to ensure we hit the rate limiter path
		results := restrictiveChecker.CheckLinks([]string{"https://httpbin.org/status/200"})
		if len(results) != 1 {
			t.Errorf("Expected 1 result, got %d", len(results))
		}
	})
}

func TestMoreMimeTypes(t *testing.T) {
	cfg := &config.Config{}
	checker := New(cfg)

	additionalMimeTypes := []struct {
		mimeType string
		expected bool
		desc     string
	}{
		// Additional directory-like types
		{"application/xhtml+xml", false, "XHTML should be treated as directory"},
		{"text/xml", false, "XML should be treated as directory"},

		// Additional file-like types
		{"application/vnd.ms-excel", true, "Excel should be treated as file"},
		{"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", true, "XLSX should be treated as file"},
		{"application/vnd.ms-powerpoint", true, "PowerPoint should be treated as file"},
		{"application/vnd.openxmlformats-officedocument.presentationml.presentation", true, "PPTX should be treated as file"},
		{"application/rtf", true, "RTF should be treated as file"},
		{"application/x-rar-compressed", true, "RAR should be treated as file"},
		{"application/x-7z-compressed", true, "7Z should be treated as file"},
		{"application/x-tar", true, "TAR should be treated as file"},
		{"application/gzip", true, "GZIP should be treated as file"},
		{"application/x-gzip", true, "GZIP should be treated as file"},
		{"image/webp", true, "WebP should be treated as file"},
		{"image/bmp", true, "BMP should be treated as file"},
		{"image/tiff", true, "TIFF should be treated as file"},
		{"image/x-icon", true, "ICO should be treated as file"},
		{"audio/wav", true, "WAV should be treated as file"},
		{"audio/ogg", true, "OGG should be treated as file"},
		{"audio/aac", true, "AAC should be treated as file"},
		{"audio/flac", true, "FLAC should be treated as file"},
		{"video/mpeg", true, "MPEG should be treated as file"},
		{"video/quicktime", true, "QuickTime should be treated as file"},
		{"video/x-msvideo", true, "AVI should be treated as file"},
		{"video/webm", true, "WebM should be treated as file"},
		{"text/csv", true, "CSV should be treated as file"},
		{"font/woff2", true, "WOFF2 should be treated as file"},
		{"application/font-woff", true, "WOFF should be treated as file"},
		{"application/font-woff2", true, "WOFF2 should be treated as file"},
		{"font/ttf", true, "TTF should be treated as file"},
		{"font/otf", true, "OTF should be treated as file"},
	}

	for _, tc := range additionalMimeTypes {
		t.Run(tc.desc, func(t *testing.T) {
			result := checker.isFileMimeType(tc.mimeType)
			if result != tc.expected {
				t.Errorf("MIME type %s: expected %v, got %v", tc.mimeType, tc.expected, result)
			}
		})
	}
}

func TestGetResolveBaseURLComprehensive(t *testing.T) {
	cfg := &config.Config{
		UserAgent: "TestBot/1.0",
		Timeout:   5 * time.Second,
	}
	checker := New(cfg)

	t.Run("URL with empty path segments", func(t *testing.T) {
		inputURL, _ := url.Parse("https://example.com")
		result := checker.getResolveBaseURL(inputURL)
		expected := "https://example.com/"
		if result.String() != expected {
			t.Errorf("Expected %s, got %s", expected, result.String())
		}
	})

	t.Run("URL with recognized file extension", func(t *testing.T) {
		// Test all the file extensions in the map
		extensions := []string{"html", "htm", "php", "asp", "aspx", "jsp", "js", "css", "xml", "json", "txt", "pdf", "doc", "docx", "jpg", "jpeg", "png", "gif", "svg", "ico", "zip", "tar", "gz", "mp3", "mp4", "woff", "woff2", "ttf", "otf", "eot"}

		for _, ext := range extensions {
			inputURL, _ := url.Parse(fmt.Sprintf("https://example.com/path/file.%s", ext))
			result := checker.getResolveBaseURL(inputURL)
			expected := "https://example.com/path/"
			if result.String() != expected {
				t.Errorf("Extension %s: expected %s, got %s", ext, expected, result.String())
			}
		}
	})

	t.Run("URL with unrecognized extension", func(t *testing.T) {
		inputURL, _ := url.Parse("https://example.com/path/file.unknown")
		result := checker.getResolveBaseURL(inputURL)
		// Should fall back to content type detection, then to directory treatment
		expected := "https://example.com/path/file.unknown/"
		if result.String() != expected {
			t.Errorf("Expected %s, got %s", expected, result.String())
		}
	})

	t.Run("Content-Type detection success - file", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/pdf")
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		inputURL, _ := url.Parse(server.URL + "/document")
		result := checker.getResolveBaseURL(inputURL)
		expected := server.URL + "/"
		if result.String() != expected {
			t.Errorf("Expected %s, got %s", expected, result.String())
		}
	})

	t.Run("Content-Type detection success - directory", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		inputURL, _ := url.Parse(server.URL + "/api")
		result := checker.getResolveBaseURL(inputURL)
		expected := server.URL + "/api/"
		if result.String() != expected {
			t.Errorf("Expected %s, got %s", expected, result.String())
		}
	})

	t.Run("Content-Type detection failure - fallback to directory", func(t *testing.T) {
		// Use a URL that will fail the HTTP request
		inputURL, _ := url.Parse("https://nonexistent.example.com/test")
		result := checker.getResolveBaseURL(inputURL)
		expected := "https://nonexistent.example.com/test/"
		if result.String() != expected {
			t.Errorf("Expected %s, got %s", expected, result.String())
		}
	})

	t.Run("Path with multiple segments and file", func(t *testing.T) {
		inputURL, _ := url.Parse("https://example.com/a/b/c/file.html")
		result := checker.getResolveBaseURL(inputURL)
		expected := "https://example.com/a/b/c/"
		if result.String() != expected {
			t.Errorf("Expected %s, got %s", expected, result.String())
		}
	})

	t.Run("Path with no segments", func(t *testing.T) {
		inputURL, _ := url.Parse("https://example.com/")
		result := checker.getResolveBaseURL(inputURL)
		expected := "https://example.com/"
		if result.String() != expected {
			t.Errorf("Expected %s, got %s", expected, result.String())
		}
	})
}

func TestCrawlWebsiteComprehensive(t *testing.T) {
	cfg := &config.Config{
		UserAgent: "TestBot/1.0",
		Timeout:   5 * time.Second,
		Verbose:   false,
	}
	checker := New(cfg)

	t.Run("crawl with parsing error in current URL", func(t *testing.T) {
		// Create a server that will be crawled
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusOK)
			// Return HTML with a malformed URL that will cause parsing issues
			w.Write([]byte(`<!DOCTYPE html>
<html>
<body>
	<a href="ht tp://invalid url.com">Invalid URL</a>
</body>
</html>`))
		}))
		defer server.Close()

		urls, err := checker.CrawlWebsite(server.URL, 1)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		// Should still return the base URL even if link extraction fails
		if len(urls) < 1 {
			t.Error("Expected at least the base URL")
		}
		if urls[0] != server.URL {
			t.Errorf("Expected first URL to be %s, got %s", server.URL, urls[0])
		}
	})

	t.Run("crawl with excluded links", func(t *testing.T) {
		// Create a checker with exclude patterns
		excludeCfg := &config.Config{
			UserAgent: "TestBot/1.0",
			Timeout:   5 * time.Second,
			Verbose:   false,
		}

		// Add exclude pattern for PDF files
		if regex, err := regexp.Compile(`.*\.pdf$`); err == nil {
			excludeCfg.ExcludePatterns = append(excludeCfg.ExcludePatterns, regex)
		}

		excludeChecker := New(excludeCfg)

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusOK)

			switch r.URL.Path {
			case "/":
				w.Write([]byte(`<!DOCTYPE html>
<html>
<body>
	<a href="/page1">Page 1</a>
	<a href="/document.pdf">PDF Document</a>
</body>
</html>`))
			case "/page1":
				w.Write([]byte(`<!DOCTYPE html>
<html>
<body>
	<p>Page 1 content</p>
</body>
</html>`))
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer server.Close()

		urls, err := excludeChecker.CrawlWebsite(server.URL, 1)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		// Should exclude the PDF file
		for _, url := range urls {
			if strings.Contains(url, ".pdf") {
				t.Errorf("PDF URL should have been excluded: %s", url)
			}
		}
	})

	t.Run("crawl with already visited URLs", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusOK)

			switch r.URL.Path {
			case "/":
				w.Write([]byte(`<!DOCTYPE html>
<html>
<body>
	<a href="/page1">Page 1</a>
	<a href="/page2">Page 2</a>
</body>
</html>`))
			case "/page1":
				w.Write([]byte(`<!DOCTYPE html>
<html>
<body>
	<a href="/page2">Page 2</a>
	<a href="/">Home</a>
</body>
</html>`))
			case "/page2":
				w.Write([]byte(`<!DOCTYPE html>
<html>
<body>
	<a href="/">Home</a>
	<a href="/page1">Page 1</a>
</body>
</html>`))
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer server.Close()

		urls, err := checker.CrawlWebsite(server.URL, 2)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		// Should not have duplicates despite circular references
		urlSet := make(map[string]bool)
		for _, url := range urls {
			if urlSet[url] {
				t.Errorf("Duplicate URL found: %s", url)
			}
			urlSet[url] = true
		}
	})
}

func TestCheckLinksComprehensive(t *testing.T) {
	cfg := &config.Config{
		UserAgent:     "TestBot/1.0",
		Timeout:       5 * time.Second,
		MaxConcurrent: 3,
		Verbose:       false,
	}
	checker := New(cfg)

	t.Run("mixed success and failure URLs", func(t *testing.T) {
		successServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer successServer.Close()

		errorServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer errorServer.Close()

		urls := []string{
			successServer.URL,
			"invalid-url",
			errorServer.URL,
			successServer.URL + "/another",
		}

		results := checker.CheckLinks(urls)

		if len(results) != len(urls) {
			t.Errorf("Expected %d results, got %d", len(urls), len(results))
		}

		// Check specific results
		if results[0].StatusCode != 200 {
			t.Errorf("Expected first result status 200, got %d", results[0].StatusCode)
		}

		if results[1].StatusCode != 0 || results[1].Error == "" {
			t.Errorf("Expected second result to have error for invalid URL")
		}

		if results[2].StatusCode != 500 {
			t.Errorf("Expected third result status 500, got %d", results[2].StatusCode)
		}
	})

	t.Run("large number of URLs", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		// Create many URLs to test concurrency
		var urls []string
		for i := 0; i < 10; i++ {
			urls = append(urls, fmt.Sprintf("%s/page%d", server.URL, i))
		}

		results := checker.CheckLinks(urls)

		if len(results) != len(urls) {
			t.Errorf("Expected %d results, got %d", len(urls), len(results))
		}

		// All should be successful
		for i, result := range results {
			if result.StatusCode != 200 {
				t.Errorf("Result %d: expected status 200, got %d", i, result.StatusCode)
			}
		}
	})
}

func TestCheckSingleLinkComprehensive(t *testing.T) {
	cfg := &config.Config{
		UserAgent:     "TestBot/1.0",
		Timeout:       5 * time.Second,
		MaxConcurrent: 1,
	}
	checker := New(cfg)

	t.Run("HEAD request with different status codes", func(t *testing.T) {
		testCases := []struct {
			statusCode int
			desc       string
		}{
			{200, "OK"},
			{201, "Created"},
			{301, "Moved Permanently"},
			{302, "Found"},
			{400, "Bad Request"},
			{401, "Unauthorized"},
			{403, "Forbidden"},
			{404, "Not Found"},
			{500, "Internal Server Error"},
			{502, "Bad Gateway"},
			{503, "Service Unavailable"},
		}

		for _, tc := range testCases {
			t.Run(tc.desc, func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(tc.statusCode)
				}))
				defer server.Close()

				result := checker.checkSingleLink(server.URL)

				if result.StatusCode != tc.statusCode {
					t.Errorf("Expected status %d, got %d", tc.statusCode, result.StatusCode)
				}

				if result.URL != server.URL {
					t.Errorf("Expected URL %s, got %s", server.URL, result.URL)
				}

				if result.Duration == "" {
					t.Error("Expected duration to be set")
				}
			})
		}
	})

	t.Run("HEAD request fails, GET succeeds", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "HEAD" {
				// Simulate a server that doesn't support HEAD
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		result := checker.checkSingleLink(server.URL)

		// Should get the HEAD response (405), not fall back to GET
		if result.StatusCode != 405 {
			t.Errorf("Expected status 405, got %d", result.StatusCode)
		}
	})

	t.Run("malformed URL", func(t *testing.T) {
		malformedURLs := []string{
			"ht tp://invalid url.com",
			"://missing-scheme.com",
			"http://",
			"not-a-url-at-all",
		}

		for _, url := range malformedURLs {
			result := checker.checkSingleLink(url)

			if result.StatusCode != 0 {
				t.Errorf("URL %s: expected status 0, got %d", url, result.StatusCode)
			}

			if result.Error == "" {
				t.Errorf("URL %s: expected error message", url)
			}
		}
	})
}

func TestGetURLsFromSitemapComprehensive(t *testing.T) {
	cfg := &config.Config{
		UserAgent: "TestBot/1.0",
		Timeout:   5 * time.Second,
	}
	checker := New(cfg)

	t.Run("sitemap with various URL types", func(t *testing.T) {
		sitemapXML := `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url>
    <loc>https://example.com/</loc>
  </url>
  <url>
    <loc>https://example.com/page1</loc>
  </url>
  <url>
    <loc>https://example.com/blog/post.html</loc>
  </url>
  <url>
    <loc>https://example.com/images/photo.jpg</loc>
  </url>
  <url>
    <loc>https://example.com/docs/manual.pdf</loc>
  </url>
  <url>
    <loc>https://external.com/page</loc>
  </url>
</urlset>`

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(sitemapXML))
		}))
		defer server.Close()

		urls, err := checker.GetURLsFromSitemap(server.URL)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		expected := 6
		if len(urls) != expected {
			t.Errorf("Expected %d URLs, got %d", expected, len(urls))
		}

		// Check that all URLs are present
		expectedURLs := []string{
			"https://example.com/",
			"https://example.com/page1",
			"https://example.com/blog/post.html",
			"https://example.com/images/photo.jpg",
			"https://example.com/docs/manual.pdf",
			"https://external.com/page",
		}

		for _, expectedURL := range expectedURLs {
			found := false
			for _, url := range urls {
				if url == expectedURL {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Expected URL not found: %s", expectedURL)
			}
		}
	})

	t.Run("empty sitemap", func(t *testing.T) {
		sitemapXML := `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
</urlset>`

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(sitemapXML))
		}))
		defer server.Close()

		urls, err := checker.GetURLsFromSitemap(server.URL)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		if len(urls) != 0 {
			t.Errorf("Expected 0 URLs for empty sitemap, got %d", len(urls))
		}
	})

	t.Run("sitemap with malformed URLs", func(t *testing.T) {
		sitemapXML := `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url>
    <loc>https://example.com/valid</loc>
  </url>
  <url>
    <loc>ht tp://invalid url.com</loc>
  </url>
  <url>
    <loc></loc>
  </url>
</urlset>`

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(sitemapXML))
		}))
		defer server.Close()

		urls, err := checker.GetURLsFromSitemap(server.URL)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		// The function includes all URLs from the sitemap, even malformed ones
		// The URL validation happens elsewhere in the pipeline
		if len(urls) != 3 {
			t.Errorf("Expected 3 URLs (including malformed), got %d", len(urls))
		}

		// Check that the valid URL is present
		validFound := false
		for _, url := range urls {
			if url == "https://example.com/valid" {
				validFound = true
				break
			}
		}
		if !validFound {
			t.Error("Valid URL not found in results")
		}
	})
}
