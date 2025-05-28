package config

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Config holds all configuration for the link checker
type Config struct {
	SitemapURL      string
	BaseURL         string
	MaxDepth        int
	Timeout         time.Duration
	UserAgent       string
	ExcludePatterns []*regexp.Regexp
	FailOnError     bool
	MaxConcurrent   int
	Verbose         bool
}

// FromEnvironment creates a Config from GitHub Action environment variables
func FromEnvironment() *Config {
	cfg := &Config{
		SitemapURL:    getEnv("INPUT_SITEMAP_URL", ""),
		BaseURL:       getEnv("INPUT_BASE_URL", ""),
		MaxDepth:      getEnvInt("INPUT_MAX_DEPTH", 3),
		Timeout:       time.Duration(getEnvInt("INPUT_TIMEOUT", 30)) * time.Second,
		UserAgent:     getEnv("INPUT_USER_AGENT", "GitHub-Action-Link-Checker/1.0"),
		FailOnError:   getEnvBool("INPUT_FAIL_ON_ERROR", true),
		MaxConcurrent: getEnvInt("INPUT_MAX_CONCURRENT", 10),
		Verbose:       getEnvBool("INPUT_VERBOSE", false),
	}

	// Parse exclude patterns
	excludeStr := getEnv("INPUT_EXCLUDE_PATTERNS", "")
	if excludeStr != "" {
		patterns := strings.Split(excludeStr, ",")
		for _, pattern := range patterns {
			pattern = strings.TrimSpace(pattern)
			if pattern != "" {
				if regex, err := regexp.Compile(pattern); err == nil {
					cfg.ExcludePatterns = append(cfg.ExcludePatterns, regex)
				}
			}
		}
	}

	return cfg
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if boolValue, err := strconv.ParseBool(value); err == nil {
			return boolValue
		}
	}
	return defaultValue
}
