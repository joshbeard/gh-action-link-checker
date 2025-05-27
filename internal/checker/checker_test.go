package checker

import (
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
	links, err := checker.extractLinksFromPage(server.URL, baseURL)
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
