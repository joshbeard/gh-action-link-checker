package config

import (
	"os"
	"testing"
	"time"
)

func TestFromEnvironment(t *testing.T) {
	// Save original environment
	originalEnv := make(map[string]string)
	envVars := []string{
		"INPUT_SITEMAP-URL",
		"INPUT_BASE-URL",
		"INPUT_MAX-DEPTH",
		"INPUT_TIMEOUT",
		"INPUT_USER-AGENT",
		"INPUT_EXCLUDE-PATTERNS",
		"INPUT_FAIL-ON-ERROR",
		"INPUT_MAX-CONCURRENT",
		"INPUT_VERBOSE",
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

	t.Run("default values", func(t *testing.T) {
		cfg := FromEnvironment()

		if cfg.SitemapURL != "" {
			t.Errorf("Expected empty SitemapURL, got %s", cfg.SitemapURL)
		}
		if cfg.BaseURL != "" {
			t.Errorf("Expected empty BaseURL, got %s", cfg.BaseURL)
		}
		if cfg.MaxDepth != 3 {
			t.Errorf("Expected MaxDepth 3, got %d", cfg.MaxDepth)
		}
		if cfg.Timeout != 30*time.Second {
			t.Errorf("Expected Timeout 30s, got %v", cfg.Timeout)
		}
		if cfg.UserAgent != "GitHub-Action-Link-Checker/1.0" {
			t.Errorf("Expected default UserAgent, got %s", cfg.UserAgent)
		}
		if cfg.FailOnError != true {
			t.Errorf("Expected FailOnError true, got %v", cfg.FailOnError)
		}
		if cfg.MaxConcurrent != 10 {
			t.Errorf("Expected MaxConcurrent 10, got %d", cfg.MaxConcurrent)
		}
		if cfg.Verbose != false {
			t.Errorf("Expected Verbose false, got %v", cfg.Verbose)
		}
		if len(cfg.ExcludePatterns) != 0 {
			t.Errorf("Expected no exclude patterns, got %d", len(cfg.ExcludePatterns))
		}
	})

	t.Run("custom values", func(t *testing.T) {
		os.Setenv("INPUT_SITEMAP-URL", "https://example.com/sitemap.xml")
		os.Setenv("INPUT_BASE-URL", "https://example.com")
		os.Setenv("INPUT_MAX-DEPTH", "5")
		os.Setenv("INPUT_TIMEOUT", "60")
		os.Setenv("INPUT_USER-AGENT", "CustomBot/1.0")
		os.Setenv("INPUT_EXCLUDE-PATTERNS", ".*\\.pdf$,.*example\\.com.*")
		os.Setenv("INPUT_FAIL-ON-ERROR", "false")
		os.Setenv("INPUT_MAX-CONCURRENT", "20")
		os.Setenv("INPUT_VERBOSE", "true")

		cfg := FromEnvironment()

		if cfg.SitemapURL != "https://example.com/sitemap.xml" {
			t.Errorf("Expected SitemapURL https://example.com/sitemap.xml, got %s", cfg.SitemapURL)
		}
		if cfg.BaseURL != "https://example.com" {
			t.Errorf("Expected BaseURL https://example.com, got %s", cfg.BaseURL)
		}
		if cfg.MaxDepth != 5 {
			t.Errorf("Expected MaxDepth 5, got %d", cfg.MaxDepth)
		}
		if cfg.Timeout != 60*time.Second {
			t.Errorf("Expected Timeout 60s, got %v", cfg.Timeout)
		}
		if cfg.UserAgent != "CustomBot/1.0" {
			t.Errorf("Expected UserAgent CustomBot/1.0, got %s", cfg.UserAgent)
		}
		if cfg.FailOnError != false {
			t.Errorf("Expected FailOnError false, got %v", cfg.FailOnError)
		}
		if cfg.MaxConcurrent != 20 {
			t.Errorf("Expected MaxConcurrent 20, got %d", cfg.MaxConcurrent)
		}
		if cfg.Verbose != true {
			t.Errorf("Expected Verbose true, got %v", cfg.Verbose)
		}
		if len(cfg.ExcludePatterns) != 2 {
			t.Errorf("Expected 2 exclude patterns, got %d", len(cfg.ExcludePatterns))
		}
	})

	t.Run("invalid values fallback to defaults", func(t *testing.T) {
		os.Setenv("INPUT_MAX-DEPTH", "invalid")
		os.Setenv("INPUT_TIMEOUT", "not-a-number")
		os.Setenv("INPUT_FAIL-ON-ERROR", "maybe")
		os.Setenv("INPUT_MAX-CONCURRENT", "abc")
		os.Setenv("INPUT_VERBOSE", "yes")

		cfg := FromEnvironment()

		if cfg.MaxDepth != 3 {
			t.Errorf("Expected MaxDepth to fallback to 3, got %d", cfg.MaxDepth)
		}
		if cfg.Timeout != 30*time.Second {
			t.Errorf("Expected Timeout to fallback to 30s, got %v", cfg.Timeout)
		}
		if cfg.FailOnError != true {
			t.Errorf("Expected FailOnError to fallback to true, got %v", cfg.FailOnError)
		}
		if cfg.MaxConcurrent != 10 {
			t.Errorf("Expected MaxConcurrent to fallback to 10, got %d", cfg.MaxConcurrent)
		}
		if cfg.Verbose != false {
			t.Errorf("Expected Verbose to fallback to false, got %v", cfg.Verbose)
		}
	})
}

func TestExcludePatterns(t *testing.T) {
	// Save and restore environment
	original := os.Getenv("INPUT_EXCLUDE-PATTERNS")
	defer func() {
		if original != "" {
			os.Setenv("INPUT_EXCLUDE-PATTERNS", original)
		} else {
			os.Unsetenv("INPUT_EXCLUDE-PATTERNS")
		}
	}()

	t.Run("valid patterns", func(t *testing.T) {
		os.Setenv("INPUT_EXCLUDE-PATTERNS", ".*\\.pdf$,.*\\.zip$,.*example\\.com.*")

		cfg := FromEnvironment()

		if len(cfg.ExcludePatterns) != 3 {
			t.Errorf("Expected 3 patterns, got %d", len(cfg.ExcludePatterns))
		}

		// Test pattern matching
		testCases := []struct {
			url      string
			expected bool
		}{
			{"https://example.com/file.pdf", true},
			{"https://example.com/file.zip", true},
			{"https://example.com/page", true},
			{"https://other.com/file.pdf", true},
			{"https://other.com/page.html", false},
		}

		for _, tc := range testCases {
			matched := false
			for _, pattern := range cfg.ExcludePatterns {
				if pattern.MatchString(tc.url) {
					matched = true
					break
				}
			}
			if matched != tc.expected {
				t.Errorf("URL %s: expected match %v, got %v", tc.url, tc.expected, matched)
			}
		}
	})

	t.Run("invalid patterns ignored", func(t *testing.T) {
		os.Setenv("INPUT_EXCLUDE-PATTERNS", ".*\\.pdf$,[invalid,.*\\.zip$")

		cfg := FromEnvironment()

		// Should only have 2 valid patterns (invalid one ignored)
		if len(cfg.ExcludePatterns) != 2 {
			t.Errorf("Expected 2 valid patterns, got %d", len(cfg.ExcludePatterns))
		}
	})

	t.Run("empty patterns", func(t *testing.T) {
		os.Setenv("INPUT_EXCLUDE-PATTERNS", "")

		cfg := FromEnvironment()

		if len(cfg.ExcludePatterns) != 0 {
			t.Errorf("Expected 0 patterns, got %d", len(cfg.ExcludePatterns))
		}
	})
}
