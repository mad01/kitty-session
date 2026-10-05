package cli

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/mad01/kitty-session/internal/claude"
	"github.com/mad01/kitty-session/internal/herdr"
	"github.com/mad01/kitty-session/internal/launcher"
	"github.com/mad01/kitty-session/internal/session"
	"github.com/mad01/kitty-session/internal/sidebar"
	"github.com/spf13/cobra"
)

var (
	importDryRun bool
	importFrom   string
	importNoOpen bool
)

var importCmd = &cobra.Command{
	Use:   "import",
	Short: "Import herdr's claude agents as ks sessions",
	Long: `Import the claude agents herdr runs into ks, so the next ks brings them back
as sessions. Reads herdr's default session file (~/.config/herdr/session.json),
writes one active ks record per claude pane with its Claude session id, and
attaches unless herdr is still running the agents.`,
	Args: cobra.NoArgs,
	RunE: runImport,
}

func init() {
	importCmd.Flags().BoolVar(&importDryRun, "dry-run", false, "print the plan, write nothing")
	importCmd.Flags().
		StringVar(&importFrom, "from", "", "herdr session.json to read (default: herdr's default session)")
	importCmd.Flags().
		BoolVar(&importNoOpen, "no-open", false, "write the records, do not start the instance")
	rootCmd.AddCommand(importCmd)
}

// idPrefixLen is how much of a Claude session id the import rows show.
const idPrefixLen = 8

// importAction is what the import decided for one herdr agent.
type importAction int

const (
	// actionImport writes a new record.
	actionImport importAction = iota
	// actionExists leaves alone a record that already has the agent's
	// Claude session id.
	actionExists
)

// label is the row prefix: in a dry run "import" says what would happen.
func (a importAction) label(dryRun bool) string {
	switch {
	case a == actionExists:
		return "exists"
	case dryRun:
		return "import"
	default:
		return "imported"
	}
}

// importItem is one herdr agent with the ks name it maps to.
type importItem struct {
	action importAction
	name   string
	agent  herdr.Agent
	// transcriptPath is where Claude Code keeps the agent's conversation;
	// transcriptOK says whether the file is there, so --resume will work.
	transcriptPath string
	transcriptOK   bool
}

func runImport(cmd *cobra.Command, args []string) error {
	path, snap, err := loadHerdrSnapshot()
	if err != nil {
		return err
	}
	agents, skipped := snap.Agents()
	if len(agents) == 0 && len(skipped) == 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "ks: no agents found in %s\n", path)
		return nil
	}
	store, items, err := planAgainstStore(agents)
	if err != nil {
		return err
	}
	if err := printImportRows(cmd.OutOrStdout(), items, skipped, importDryRun); err != nil {
		return err
	}
	printWarnings(cmd, transcriptWarnings(items))

	toImport := countAction(items, actionImport)
	if importDryRun {
		fmt.Fprintf(
			cmd.OutOrStdout(),
			"ks: dry run, nothing written: %d to import, %d already present, %d skipped\n",
			toImport,
			len(items)-toImport,
			len(skipped),
		)
		return nil
	}
	imported, err := writeImports(store, items)
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "ks: %d imported, %d already present, %d skipped\n",
		imported, len(items)-imported, len(skipped))
	if imported == 0 {
		return nil
	}
	return openImported(cmd, filepath.Dir(path))
}

// loadHerdrSnapshot reads the herdr session file named by --from, else the
// default session's, and returns its path with the parsed snapshot.
func loadHerdrSnapshot() (string, *herdr.Snapshot, error) {
	path, err := importSource()
	if err != nil {
		return "", nil, err
	}
	snap, err := herdr.Load(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil, fmt.Errorf(
			"no herdr session file at %s; pass --from <session.json> to read another", path,
		)
	}
	if err != nil {
		return "", nil, err
	}
	return path, snap, nil
}

// importSource is the herdr session file to read: --from, else the default
// session's.
func importSource() (string, error) {
	if importFrom != "" {
		return filepath.Abs(importFrom)
	}
	return herdr.DefaultPath()
}

// planAgainstStore opens the session store and plans the import against the
// records already in it.
func planAgainstStore(agents []herdr.Agent) (*session.Store, []importItem, error) {
	store, err := session.NewStore()
	if err != nil {
		return nil, nil, err
	}
	existing, err := store.List()
	if err != nil {
		return nil, nil, err
	}
	items, err := planImport(agents, existing)
	if err != nil {
		return nil, nil, err
	}
	return store, items, nil
}

// planImport maps every herdr agent to a ks record: the one that already
// carries its Claude session id, or a new name free of collisions with the
// existing records and with the other agents of this import.
func planImport(agents []herdr.Agent, existing []*session.Session) ([]importItem, error) {
	byClaudeID := map[string]string{}
	taken := map[string]bool{}
	for _, s := range existing {
		taken[s.Name] = true
		if s.ClaudeSessionID != "" {
			byClaudeID[s.ClaudeSessionID] = s.Name
		}
	}
	items := make([]importItem, 0, len(agents))
	for _, a := range agents {
		if name, ok := byClaudeID[a.SessionID]; ok {
			items = append(items, importItem{action: actionExists, name: name, agent: a})
			continue
		}
		name := freeName(importName(a), taken)
		taken[name] = true
		byClaudeID[a.SessionID] = name
		transcript, err := claude.TranscriptPath(a.Dir, a.SessionID)
		if err != nil {
			return nil, err
		}
		_, statErr := os.Stat(transcript)
		items = append(items, importItem{
			action:         actionImport,
			name:           name,
			agent:          a,
			transcriptPath: transcript,
			transcriptOK:   statErr == nil,
		})
	}
	return items, nil
}

// importName is the ks name for a herdr agent: the workspace or tab name
// its user chose, else the same directory-and-branch name ks new suggests,
// else one built from the Claude session id.
func importName(a herdr.Agent) string {
	if name := launcher.SanitizeName(a.NameHint); name != "" {
		return name
	}
	if name := launcher.SuggestName(a.Dir); name != "" {
		return name
	}
	return "herdr-" + shortID(a.SessionID)
}

// freeName returns base when no record has it, else base-2, base-3, and so
// on: the lowest suffix not taken, so a rerun names things the same way.
func freeName(base string, taken map[string]bool) string {
	if !taken[base] {
		return base
	}
	for n := 2; ; n++ {
		candidate := fmt.Sprintf("%s-%d", base, n)
		if !taken[candidate] {
			return candidate
		}
	}
}

// shortID is the prefix of a Claude session id the rows show.
func shortID(id string) string {
	if len(id) <= idPrefixLen {
		return id
	}
	return id[:idPrefixLen]
}

// printImportRows writes one aligned line per agent, then one per skipped
// pane with its reason.
func printImportRows(w io.Writer, items []importItem, skipped []herdr.Skip, dryRun bool) error {
	home, _ := os.UserHomeDir()
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, it := range items {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", it.action.label(dryRun), it.name,
			sidebar.ShortenHome(it.agent.Dir, home), shortID(it.agent.SessionID))
	}
	for _, s := range skipped {
		fmt.Fprintf(tw, "skipped\t\t%s\t%s\n", sidebar.ShortenHome(s.Dir, home), s.Reason)
	}
	if err := tw.Flush(); err != nil {
		return fmt.Errorf("cannot write import rows: %w", err)
	}
	return nil
}

// transcriptWarnings names the new records whose transcript is gone. The
// launcher then falls back to --continue or a bare claude, so the session
// comes back without its conversation.
func transcriptWarnings(items []importItem) []error {
	var warnings []error
	for _, it := range items {
		if it.action == actionImport && !it.transcriptOK {
			warnings = append(warnings,
				fmt.Errorf("%s: transcript missing, will start fresh", it.name))
		}
	}
	return warnings
}

func countAction(items []importItem, action importAction) int {
	n := 0
	for _, it := range items {
		if it.action == action {
			n++
		}
	}
	return n
}

// writeImports saves an active record with no kitty ids for every item to
// import, which is exactly what attach resumes. It returns how many it wrote.
func writeImports(store *session.Store, items []importItem) (int, error) {
	written := 0
	for _, it := range items {
		if it.action != actionImport {
			continue
		}
		sess := session.New(it.name, it.agent.Dir, 0, 0)
		sess.ClaudeSessionID = it.agent.SessionID
		sess.ClaudeTranscriptPath = it.transcriptPath
		if err := store.Save(sess); err != nil {
			return written, fmt.Errorf("cannot save %s: %w", it.name, err)
		}
		written++
	}
	return written, nil
}

// openImported attaches, bringing the new records up, unless herdr still
// runs the agents (a second claude on the same transcript would clash) or
// --no-open asked for the records only. herdrDir holds herdr's socket.
func openImported(cmd *cobra.Command, herdrDir string) error {
	if herdr.Running(herdrDir) {
		fmt.Fprintln(cmd.ErrOrStderr(),
			"ks: herdr is still running these agents; not opening them.")
		fmt.Fprintln(cmd.ErrOrStderr(),
			"    Stop it with: herdr session stop default   then run: ks")
		return nil
	}
	if importNoOpen {
		return nil
	}
	w, err := ensureWiring(false)
	if err != nil {
		return err
	}
	res, err := w.launcher.Attach()
	if err != nil {
		return err
	}
	printAttachResult(cmd, res)
	return nil
}
