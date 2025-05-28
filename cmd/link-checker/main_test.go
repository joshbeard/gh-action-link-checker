package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/joshbeard/link-validator/internal/config"
)

func TestSetOutput(t *testing.T) {
	// Create a temporary file to simulate GITHUB_OUTPUT
	tmpFile, err := os.CreateTemp("", "github_output_test")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	// Set GITHUB_OUTPUT environment variable
	originalOutput := os.Getenv("GITHUB_OUTPUT")
	os.Setenv("GITHUB_OUTPUT", tmpFile.Name())
	defer func() {
		if originalOutput != "" {
			os.Setenv("GITHUB_OUTPUT", originalOutput)
		} else {
			os.Unsetenv("GITHUB_OUTPUT")
		}
	}()

	t.Run("simple output", func(t *testing.T) {
		setOutput("test-key", "test-value")

		// Read the file content
		content, err := os.ReadFile(tmpFile.Name())
		if err != nil {
			t.Fatalf("Failed to read output file: %v", err)
		}

		expected := "test-key=test-value\n"
		if string(content) != expected {
			t.Errorf("Expected %q, got %q", expected, string(content))
		}
	})

	t.Run("multiline output", func(t *testing.T) {
		// Clear the file
		if err := tmpFile.Truncate(0); err != nil {
			t.Fatalf("Failed to truncate file: %v", err)
		}
		if _, err := tmpFile.Seek(0, 0); err != nil {
			t.Fatalf("Failed to seek file: %v", err)
		}

		multilineValue := "line1\nline2\nline3"
		setOutput("multiline-key", multilineValue)

		// Read the file content
		content, err := os.ReadFile(tmpFile.Name())
		if err != nil {
			t.Fatalf("Failed to read output file: %v", err)
		}

		expected := "multiline-key<<EOF\nline1\nline2\nline3\nEOF\n"
		if string(content) != expected {
			t.Errorf("Expected %q, got %q", expected, string(content))
		}
	})
}

func TestMainIntegration(t *testing.T) {
	// Create a test server with a simple sitemap
	sitemapXML := `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url>
    <loc>%s/page1</loc>
  </url>
  <url>
    <loc>%s/page2</loc>
  </url>
</urlset>`

	// Create servers for the pages
	page1Server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte("Page 1 content")); err != nil {
			http.Error(w, "Failed to write response", http.StatusInternalServerError)
		}
	}))
	defer page1Server.Close()

	page2Server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		if _, err := w.Write([]byte("Not Found")); err != nil {
			http.Error(w, "Failed to write response", http.StatusInternalServerError)
		}
	}))
	defer page2Server.Close()

	// Create sitemap server
	sitemapServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusOK)
		content := fmt.Sprintf(sitemapXML, page1Server.URL, page2Server.URL)
		if _, err := w.Write([]byte(content)); err != nil {
			http.Error(w, "Failed to write response", http.StatusInternalServerError)
		}
	}))
	defer sitemapServer.Close()

	// Save original environment
	originalEnv := make(map[string]string)
	envVars := []string{
		"INPUT_SITEMAP_URL",
		"INPUT_BASE_URL",
		"INPUT_TIMEOUT",
		"INPUT_MAX_CONCURRENT",
		"INPUT_VERBOSE",
		"INPUT_FAIL_ON_ERROR",
		"GITHUB_OUTPUT",
	}

	for _, env := range envVars {
		originalEnv[env] = os.Getenv(env)
		os.Unsetenv(env)
	}

	// Restore environment after test
	defer func() {
		for _, env := range envVars {
			if val, exists := originalEnv[env]; exists {
				os.Setenv(env, val)
			} else {
				os.Unsetenv(env)
			}
		}
	}()

	// Create temporary output file
	tmpFile, err := os.CreateTemp("", "github_output_integration_test")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	// Set environment variables for the test
	os.Setenv("INPUT_SITEMAP_URL", sitemapServer.URL)
	os.Setenv("INPUT_TIMEOUT", "10")
	os.Setenv("INPUT_MAX_CONCURRENT", "2")
	os.Setenv("INPUT_VERBOSE", "false")
	os.Setenv("INPUT_FAIL_ON_ERROR", "false")
	os.Setenv("GITHUB_OUTPUT", tmpFile.Name())

	// This test would normally call main(), but since main() calls os.Exit(),
	// we'll test the core logic instead by calling the same functions main() uses
	// In a real integration test, you might use a separate test binary or mock os.Exit

	// For now, let's just verify the environment setup works
	cfg := config.FromEnvironment()

	if cfg.SitemapURL != sitemapServer.URL {
		t.Errorf("Expected sitemap URL %s, got %s", sitemapServer.URL, cfg.SitemapURL)
	}
	if cfg.Timeout.Seconds() != 10 {
		t.Errorf("Expected timeout 10s, got %v", cfg.Timeout)
	}
	if cfg.MaxConcurrent != 2 {
		t.Errorf("Expected max concurrent 2, got %d", cfg.MaxConcurrent)
	}
	if cfg.Verbose != false {
		t.Errorf("Expected verbose false, got %v", cfg.Verbose)
	}
	if cfg.FailOnError != false {
		t.Errorf("Expected fail on error false, got %v", cfg.FailOnError)
	}
}
