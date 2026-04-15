package finder

// Repo represents a discovered git repository.
type Repo struct {
	Name   string // org/repo extracted from remote URL
	Path   string // absolute filesystem path
	Remote string // full origin remote URL (e.g. "git@git.example.com:team/service.git")
	Host   string // extracted hostname (e.g. "github.com", "git.example.com")
}
