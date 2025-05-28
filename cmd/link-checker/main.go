package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/joshbeard/link-validator/internal/checker"
	"github.com/joshbeard/link-validator/internal/config"
)

// version is set via ldflags during build
var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
)

func main() {
	// Parse command line flags
	var showVersion bool
	var showHelp bool

	flag.BoolVar(&showVersion, "version", false, "Show version information")
	flag.BoolVar(&showHelp, "help", false, "Show help information")

	// Override the default usage function to provide better help
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Link Validator\n\n")
		fmt.Fprintf(os.Stderr, "A tool to check for broken links in websites by crawling or using sitemaps.\n\n")
		fmt.Fprintf(os.Stderr, "Usage: %s [options]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nEnvironment Variables (GitHub Action inputs):\n")
		fmt.Fprintf(os.Stderr, "  INPUT_SITEMAP_URL      URL of the sitemap to check (alternative to base-url)\n")
		fmt.Fprintf(os.Stderr, "  INPUT_BASE_URL         Base URL to start crawling from (alternative to sitemap-url)\n")
		fmt.Fprintf(os.Stderr, "  INPUT_MAX_DEPTH        Maximum crawl depth (default: 3)\n")
		fmt.Fprintf(os.Stderr, "  INPUT_TIMEOUT          Request timeout in seconds (default: 30)\n")
		fmt.Fprintf(os.Stderr, "  INPUT_USER_AGENT       User agent string (default: GitHub-Action-Link-Checker/1.0)\n")
		fmt.Fprintf(os.Stderr, "  INPUT_EXCLUDE_PATTERNS Comma-separated regex patterns to exclude URLs\n")
		fmt.Fprintf(os.Stderr, "  INPUT_FAIL_ON_ERROR    Exit with error code if broken links found (default: true)\n")
		fmt.Fprintf(os.Stderr, "  INPUT_MAX_CONCURRENT   Maximum concurrent requests (default: 10)\n")
		fmt.Fprintf(os.Stderr, "  INPUT_VERBOSE          Enable verbose output (default: false)\n")
		fmt.Fprintf(os.Stderr, "\nNote: Command line flags take precedence over environment variables.\n")
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  # Check links from sitemap using flags\n")
		fmt.Fprintf(os.Stderr, "  %s --sitemap-url https://example.com/sitemap.xml\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  # Crawl website using flags\n")
		fmt.Fprintf(os.Stderr, "  %s --base-url https://example.com --max-depth 2 --verbose\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  # Check links from sitemap using environment variables\n")
		fmt.Fprintf(os.Stderr, "  INPUT_SITEMAP_URL=https://example.com/sitemap.xml %s\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  # Crawl website using environment variables\n")
		fmt.Fprintf(os.Stderr, "  INPUT_BASE_URL=https://example.com INPUT_MAX_DEPTH=2 %s\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  # Show version\n")
		fmt.Fprintf(os.Stderr, "  %s --version\n\n", os.Args[0])
	}

	// Define config flags (but don't parse yet)
	var (
		sitemapURL      = flag.String("sitemap-url", "", "URL of the sitemap to check")
		baseURL         = flag.String("base-url", "", "Base URL to start crawling from")
		maxDepth        = flag.Int("max-depth", 3, "Maximum crawl depth")
		timeout         = flag.Int("timeout", 30, "Request timeout in seconds")
		userAgent       = flag.String("user-agent", "GitHub-Action-Link-Checker/1.0", "User agent string")
		excludePatterns = flag.String("exclude-patterns", "", "Comma-separated regex patterns to exclude URLs")
		failOnError     = flag.Bool("fail-on-error", true, "Exit with error code if broken links found")
		maxConcurrent   = flag.Int("max-concurrent", 10, "Maximum concurrent requests")
		verbose         = flag.Bool("verbose", false, "Enable verbose output")
	)

	flag.Parse()

	if showHelp {
		flag.Usage()
		os.Exit(0)
	}

	if showVersion {
		fmt.Printf("link-checker version %s\n", version)
		if commit != "unknown" {
			fmt.Printf("commit: %s\n", commit)
		}
		if buildDate != "unknown" {
			fmt.Printf("built: %s\n", buildDate)
		}
		os.Exit(0)
	}

	// Create config from flags with environment variable fallbacks
	cfg := &config.Config{
		SitemapURL:    getValueOrEnv(*sitemapURL, "INPUT_SITEMAP_URL", "", "sitemap-url"),
		BaseURL:       getValueOrEnv(*baseURL, "INPUT_BASE_URL", "", "base-url"),
		MaxDepth:      getIntValueOrEnv(*maxDepth, "INPUT_MAX_DEPTH", 3, "max-depth"),
		Timeout:       time.Duration(getIntValueOrEnv(*timeout, "INPUT_TIMEOUT", 30, "timeout")) * time.Second,
		UserAgent:     getValueOrEnv(*userAgent, "INPUT_USER_AGENT", "GitHub-Action-Link-Checker/1.0", "user-agent"),
		FailOnError:   getBoolValueOrEnv(*failOnError, "INPUT_FAIL_ON_ERROR", true, "fail-on-error"),
		MaxConcurrent: getIntValueOrEnv(*maxConcurrent, "INPUT_MAX_CONCURRENT", 10, "max-concurrent"),
		Verbose:       getBoolValueOrEnv(*verbose, "INPUT_VERBOSE", false, "verbose"),
	}

	// Parse exclude patterns
	excludePatternsStr := getValueOrEnv(*excludePatterns, "INPUT_EXCLUDE_PATTERNS", "", "exclude-patterns")
	if excludePatternsStr != "" {
		patterns := strings.Split(excludePatternsStr, ",")
		for _, pattern := range patterns {
			pattern = strings.TrimSpace(pattern)
			if pattern != "" {
				if regex, err := regexp.Compile(pattern); err == nil {
					cfg.ExcludePatterns = append(cfg.ExcludePatterns, regex)
				}
			}
		}
	}

	if cfg.SitemapURL == "" && cfg.BaseURL == "" {
		fmt.Fprintf(os.Stderr, "Error: Either sitemap-url or base-url must be provided\n\n")
		fmt.Fprintf(os.Stderr, "Use --help for usage information.\n")
		os.Exit(1)
	}

	linkChecker := checker.New(cfg)

	var urls []string
	var err error

	if cfg.SitemapURL != "" {
		fmt.Printf("Fetching URLs from sitemap: %s\n", cfg.SitemapURL)
		urls, err = linkChecker.GetURLsFromSitemap(cfg.SitemapURL)
		if err != nil {
			log.Fatalf("Failed to fetch sitemap: %v", err)
		}
	} else {
		fmt.Printf("Crawling website starting from: %s\n", cfg.BaseURL)
		urls, err = linkChecker.CrawlWebsite(cfg.BaseURL, cfg.MaxDepth)
		if err != nil {
			log.Fatalf("Failed to crawl website: %v", err)
		}
	}

	fmt.Printf("Found %d URLs to check\n", len(urls))

	results := linkChecker.CheckLinks(urls)

	brokenLinks := []checker.LinkResult{}
	for _, result := range results {
		if result.StatusCode >= 400 {
			brokenLinks = append(brokenLinks, result)
		}
	}

	// Output results
	fmt.Printf("\n=== Link Check Results ===\n")
	fmt.Printf("Total links checked: %d\n", len(results))
	fmt.Printf("Broken links found: %d\n", len(brokenLinks))

	if len(brokenLinks) > 0 {
		fmt.Printf("\n=== Broken Links ===\n")
		for _, link := range brokenLinks {
			fmt.Printf("❌ %s (Status: %d) - %s\n", link.URL, link.StatusCode, link.Error)
		}
	} else {
		fmt.Printf("✅ No broken links found!\n")
	}

	// Set GitHub Action outputs
	setOutput("total-links-checked", strconv.Itoa(len(results)))
	setOutput("broken-links-count", strconv.Itoa(len(brokenLinks)))

	brokenLinksJSON, _ := json.Marshal(brokenLinks)
	setOutput("broken-links", string(brokenLinksJSON))

	// Exit with error if broken links found and fail-on-error is true
	if len(brokenLinks) > 0 && cfg.FailOnError {
		os.Exit(1)
	}
}

func setOutput(name, value string) {
	if githubOutput := os.Getenv("GITHUB_OUTPUT"); githubOutput != "" {
		f, err := os.OpenFile(githubOutput, os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			log.Printf("Failed to open GITHUB_OUTPUT file: %v", err)
			return
		}
		defer f.Close()

		// Handle multiline values
		if strings.Contains(value, "\n") {
			delimiter := "EOF"
			fmt.Fprintf(f, "%s<<%s\n%s\n%s\n", name, delimiter, value, delimiter)
		} else {
			fmt.Fprintf(f, "%s=%s\n", name, value)
		}
	}
}

// Helper functions for flag/environment variable precedence
func getValueOrEnv(flagValue, envKey, defaultValue, flagName string) string {
	// Check if flag was explicitly set
	flagSet := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == flagName {
			flagSet = true
		}
	})

	if flagSet {
		return flagValue
	}
	if value := os.Getenv(envKey); value != "" {
		return value
	}
	return defaultValue
}

func getIntValueOrEnv(flagValue int, envKey string, defaultValue int, flagName string) int {
	// Check if flag was explicitly set
	flagSet := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == flagName {
			flagSet = true
		}
	})

	if flagSet {
		return flagValue
	}
	if value := os.Getenv(envKey); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

func getBoolValueOrEnv(flagValue bool, envKey string, defaultValue bool, flagName string) bool {
	// Check if flag was explicitly set
	flagSet := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == flagName {
			flagSet = true
		}
	})

	if flagSet {
		return flagValue
	}
	if value := os.Getenv(envKey); value != "" {
		if boolValue, err := strconv.ParseBool(value); err == nil {
			return boolValue
		}
	}
	return defaultValue
}
