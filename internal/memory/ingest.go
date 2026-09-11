package memory

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var (
	scriptStyleRe = regexp.MustCompile(`(?is)<script.*?>.*?</script>|<style.*?>.*?</style>`)
	tagRe         = regexp.MustCompile(`(?is)<.*?>`)
	whitespaceRe  = regexp.MustCompile(`[ \t]+`)
	newlinesRe    = regexp.MustCompile(`\n{3,}`)
)

// FetchReference downloads a URL, strips HTML tags, and returns clean text
// suitable for LLM context processing (saving tokens).
func FetchReference(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}
	
	// Add user-agent to avoid simple blocks
	req.Header.Set("User-Agent", "sv-memory/1.0 (Web Ingest)")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed fetching URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	
	text := string(body)

	// If it's already markdown or raw text (e.g. raw.githubusercontent), return as is.
	// But generally we strip HTML.
	if strings.Contains(resp.Header.Get("Content-Type"), "text/html") {
		// Strip scripts and styles
		text = scriptStyleRe.ReplaceAllString(text, "")
		
		// Replace some structural tags with newlines
		text = strings.ReplaceAll(text, "</p>", "\n\n")
		text = strings.ReplaceAll(text, "<br>", "\n")
		text = strings.ReplaceAll(text, "<br/>", "\n")
		text = strings.ReplaceAll(text, "</div>", "\n")
		text = strings.ReplaceAll(text, "</h1>", "\n\n")
		text = strings.ReplaceAll(text, "</h2>", "\n\n")
		text = strings.ReplaceAll(text, "</h3>", "\n\n")
		
		// Strip remaining HTML tags
		text = tagRe.ReplaceAllString(text, "")
		
		// Decode basic HTML entities
		text = strings.ReplaceAll(text, "&nbsp;", " ")
		text = strings.ReplaceAll(text, "&lt;", "<")
		text = strings.ReplaceAll(text, "&gt;", ">")
		text = strings.ReplaceAll(text, "&amp;", "&")
		text = strings.ReplaceAll(text, "&quot;", "\"")
	}

	// Normalize whitespace
	text = whitespaceRe.ReplaceAllString(text, " ")
	text = newlinesRe.ReplaceAllString(text, "\n\n")
	text = strings.TrimSpace(text)

	// Truncate to a reasonable limit for tokens (e.g., 200KB ~ 50k tokens)
	if len(text) > 200000 {
		text = text[:200000] + "\n... (truncated for context limits)"
	}

	return text, nil
}
