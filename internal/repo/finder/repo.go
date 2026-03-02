package finder

// Repo represents a discovered git repository.
type Repo struct {
	Name string // org/repo extracted from remote URL
	Path string // absolute filesystem path
}
