package accutil

import (
	"fmt"
	"strings"

	"gitlab.com/accumulatenetwork/accumulate/pkg/url"
)

// ParseURL parses an Accumulate URL string and returns a URL object
func ParseURL(s string) (*url.URL, error) {
	if s == "" {
		return nil, fmt.Errorf("URL string cannot be empty")
	}

	// Parse the URL using Accumulate's URL parser
	u, err := url.Parse(s)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Accumulate URL %q: %w", s, err)
	}

	return u, nil
}

// MustParseURL parses an Accumulate URL string and panics on error
// This should only be used for URLs that are known to be valid (e.g., constants)
func MustParseURL(s string) *url.URL {
	u, err := ParseURL(s)
	if err != nil {
		panic(fmt.Sprintf("invalid URL: %v", err))
	}
	return u
}

// IsADI checks if the URL represents an Accumulate Decentralized Identity (ADI)
func IsADI(u *url.URL) bool {
	if u == nil {
		return false
	}

	// An ADI is typically a top-level URL with no path segments
	// e.g., acc://example.acme
	path := strings.Trim(u.Path, "/")
	return path == "" && u.Authority != ""
}

// IsKeyBook checks if the URL represents a key book
func IsKeyBook(u *url.URL) bool {
	if u == nil {
		return false
	}

	// Key books typically end with "/book" or have "book" as a path segment
	// e.g., acc://example.acme/book
	path := strings.Trim(u.Path, "/")
	pathSegments := strings.Split(path, "/")

	// Check if the last segment is "book" or contains "book"
	if len(pathSegments) > 0 {
		lastSegment := pathSegments[len(pathSegments)-1]
		return lastSegment == "book" || strings.Contains(lastSegment, "book")
	}

	return false
}

// IsKeyPage checks if the URL represents a key page
func IsKeyPage(u *url.URL) bool {
	if u == nil {
		return false
	}

	// Key pages are typically under a key book path
	// e.g., acc://example.acme/book/1 or acc://example.acme/book/page1
	path := strings.Trim(u.Path, "/")
	pathSegments := strings.Split(path, "/")

	// Must have at least 2 segments and contain "book"
	if len(pathSegments) >= 2 {
		for i, segment := range pathSegments {
			if segment == "book" || strings.Contains(segment, "book") {
				// If we found "book" and there are more segments after it,
				// it's likely a key page
				return i < len(pathSegments)-1
			}
		}
	}

	return false
}

// ValidateContractAddr validates that a URL is a valid contract address
// Expected format: acc://<adi>/accumen/<contract>
func ValidateContractAddr(u *url.URL) error {
	if u == nil {
		return fmt.Errorf("URL cannot be nil")
	}

	if u.Authority == "" {
		return fmt.Errorf("contract address must have an authority (ADI): %s", u)
	}

	path := strings.Trim(u.Path, "/")
	if path == "" {
		return fmt.Errorf("contract address must have a path: %s", u)
	}

	pathSegments := strings.Split(path, "/")
	if len(pathSegments) < 2 {
		return fmt.Errorf("contract address must have at least 2 path segments: %s", u)
	}

	// Check if the first segment is "accumen" (indicating it's managed by Accumen)
	if pathSegments[0] != "accumen" {
		return fmt.Errorf("contract address must start with 'accumen': %s", u)
	}

	// Validate that the contract name is not empty
	contractName := pathSegments[1]
	if contractName == "" {
		return fmt.Errorf("contract name cannot be empty: %s", u)
	}

	// Optional: validate contract name format (alphanumeric, hyphens, underscores)
	if !isValidContractName(contractName) {
		return fmt.Errorf("contract name contains invalid characters: %s", contractName)
	}

	return nil
}

// isValidContractName checks if a contract name contains only valid characters
func isValidContractName(name string) bool {
	if name == "" {
		return false
	}

	for _, r := range name {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			 (r >= '0' && r <= '9') || r == '-' || r == '_') {
			return false
		}
	}

	return true
}

// Canonicalize returns the canonical string representation of an Accumulate URL
func Canonicalize(u *url.URL) string {
	if u == nil {
		return ""
	}

	// Use the URL's built-in String() method, but ensure consistent formatting
	canonical := u.String()

	// Normalize the scheme to lowercase
	if strings.HasPrefix(canonical, "ACC://") {
		canonical = "acc://" + canonical[6:]
	}

	// Remove trailing slash if present and not needed
	if strings.HasSuffix(canonical, "/") && len(canonical) > 6 { // 6 = len("acc://")
		canonical = strings.TrimSuffix(canonical, "/")
	}

	return canonical
}

// IsDataAccount checks if the URL represents a data account
func IsDataAccount(u *url.URL) bool {
	if u == nil {
		return false
	}

	// Data accounts are typically leaf URLs that are not ADIs, key books, or key pages
	return !IsADI(u) && !IsKeyBook(u) && !IsKeyPage(u) && u.Path != ""
}

// IsTokenAccount checks if the URL represents a token account
func IsTokenAccount(u *url.URL) bool {
	if u == nil {
		return false
	}

	// Token accounts often contain "tokens" in their path
	path := strings.Trim(u.Path, "/")
	return strings.Contains(strings.ToLower(path), "token")
}

// GetADI extracts the ADI portion from any Accumulate URL
func GetADI(u *url.URL) (*url.URL, error) {
	if u == nil {
		return nil, fmt.Errorf("URL cannot be nil")
	}

	if u.Authority == "" {
		return nil, fmt.Errorf("URL must have an authority: %s", u)
	}

	// Create a new URL with just the scheme and authority
	adiURL := &url.URL{
		Scheme:    u.Scheme,
		Authority: u.Authority,
	}

	return adiURL, nil
}

// GetContractName extracts the contract name from a contract URL
// Expected format: acc://<adi>/accumen/<contract>
func GetContractName(u *url.URL) (string, error) {
	if err := ValidateContractAddr(u); err != nil {
		return "", err
	}

	path := strings.Trim(u.Path, "/")
	pathSegments := strings.Split(path, "/")

	// We validated that there are at least 2 segments and first is "accumen"
	return pathSegments[1], nil
}

// BuildContractURL constructs a contract URL from ADI and contract name
func BuildContractURL(adi, contractName string) (*url.URL, error) {
	if adi == "" {
		return nil, fmt.Errorf("ADI cannot be empty")
	}

	if contractName == "" {
		return nil, fmt.Errorf("contract name cannot be empty")
	}

	if !isValidContractName(contractName) {
		return nil, fmt.Errorf("contract name contains invalid characters: %s", contractName)
	}

	// Remove acc:// prefix if present in ADI
	if strings.HasPrefix(strings.ToLower(adi), "acc://") {
		adi = adi[6:]
	}

	// Construct the contract URL
	urlString := fmt.Sprintf("acc://%s/accumen/%s", adi, contractName)

	return ParseURL(urlString)
}

// SplitPath splits a URL path into segments, removing empty segments
func SplitPath(u *url.URL) []string {
	if u == nil || u.Path == "" {
		return nil
	}

	path := strings.Trim(u.Path, "/")
	if path == "" {
		return nil
	}

	segments := strings.Split(path, "/")

	// Filter out empty segments
	result := make([]string, 0, len(segments))
	for _, segment := range segments {
		if segment != "" {
			result = append(result, segment)
		}
	}

	return result
}

// JoinPath creates a URL by joining path segments to a base URL
func JoinPath(base *url.URL, segments ...string) *url.URL {
	if base == nil {
		return nil
	}

	// Create a copy of the base URL
	result := &url.URL{
		Scheme:    base.Scheme,
		Authority: base.Authority,
		UserInfo:  base.UserInfo,
		Host:      base.Host,
		Port:      base.Port,
		Path:      base.Path,
		Query:     base.Query,
		Fragment:  base.Fragment,
	}

	// Add segments to the path
	basePath := strings.Trim(result.Path, "/")
	allSegments := []string{}

	if basePath != "" {
		allSegments = append(allSegments, basePath)
	}

	for _, segment := range segments {
		segment = strings.Trim(segment, "/")
		if segment != "" {
			allSegments = append(allSegments, segment)
		}
	}

	if len(allSegments) > 0 {
		result.Path = "/" + strings.Join(allSegments, "/")
	} else {
		result.Path = ""
	}

	return result
}