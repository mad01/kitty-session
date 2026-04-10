// Package search provides local code search backed by zoekt indexes.
// It indexes the working tree of git repositories discovered by ks,
// and supports the same query syntax as the code-search MCP server.
package search

// Match represents a single search hit in a file.
type Match struct {
	// Repo is the org/repo name (e.g. "mad01/kitty-session").
	Repo string `json:"repo"`
	// RepoPath is the absolute filesystem path to the repo root.
	RepoPath string `json:"repo_path"`
	// File is the path relative to the repo root.
	File string `json:"file"`
	// Line is the 1-based line number of the match.
	Line int `json:"line"`
	// Column is the 1-based column of the match start.
	Column int `json:"column"`
	// Text is the content of the matching line.
	Text string `json:"text"`
	// Before holds context lines before the match (when requested).
	Before string `json:"before,omitempty"`
	// After holds context lines after the match (when requested).
	After string `json:"after,omitempty"`
}

// SearchOptions configures a search query.
// Field names align with the code-search MCP tool parameters.
type SearchOptions struct {
	// Pattern is the zoekt query string (regex, boolean, filters).
	Pattern string
	// RepoFilter restricts results to repos matching this regex.
	RepoFilter string
	// FileFilter restricts results to files matching this regex.
	FileFilter string
	// Lang restricts results to files of this language.
	Lang string
	// CaseSensitive disables smart case matching.
	CaseSensitive bool
	// Limit caps the number of files returned (default 50).
	Limit int
	// ContextLines is the number of context lines around each match.
	// Only applies when OutputMode is "content".
	ContextLines int
	// OutputMode controls output verbosity: "files_with_matches" or "content".
	OutputMode string
}

// CountOptions configures a count query.
type CountOptions struct {
	// Pattern is the zoekt query string.
	Pattern string
	// RepoFilter restricts to repos matching this regex.
	RepoFilter string
	// Lang restricts to files of this language.
	Lang string
	// GroupBy groups counts by "repo" or "language".
	GroupBy string
}

// CountResult holds the result of a count query.
type CountResult struct {
	// Group is the repo name or language, depending on GroupBy.
	Group string `json:"group"`
	// Count is the number of matches in this group.
	Count int `json:"count"`
}

// QueryInfo holds the result of parsing and validating a query.
type QueryInfo struct {
	// Valid indicates whether the query parsed successfully.
	Valid bool `json:"valid"`
	// Parsed is the string representation of the parsed query tree.
	Parsed string `json:"parsed,omitempty"`
	// Error is the parse error message, if any.
	Error string `json:"error,omitempty"`
	// Hint is a suggestion for fixing the query.
	Hint string `json:"hint,omitempty"`
}

// DoctorReport holds the result of a health check.
type DoctorReport struct {
	IndexDir       string       `json:"index_dir"`
	IndexSizeBytes int64        `json:"index_size_bytes"`
	TotalRepos     int          `json:"total_repos"`
	Issues         []RepoIssue  `json:"issues"`
	Healthy        []RepoHealth `json:"healthy"`
}

// RepoIssue describes a problem with a repo's index.
type RepoIssue struct {
	Repo           string `json:"repo"`
	Path           string `json:"path"`
	Type           string `json:"type"` // "stale", "missing", "dirty"
	Dirty          bool   `json:"dirty"`
	ModifiedFiles  int    `json:"modified_files,omitempty"`
	UntrackedFiles int    `json:"untracked_files,omitempty"`
	IndexAge       string `json:"index_age,omitempty"`
	Message        string `json:"message"`
}

// RepoHealth describes a healthy repo index.
type RepoHealth struct {
	Repo     string `json:"repo"`
	Path     string `json:"path"`
	IndexAge string `json:"index_age"`
}
