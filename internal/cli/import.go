package cli

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"text/tabwriter"
	"time"

	"github.com/mad01/kitty-session/internal/claude"
	"github.com/mad01/kitty-session/internal/herdr"
	"github.com/mad01/kitty-session/internal/kitty"
	"github.com/mad01/kitty-session/internal/launcher"
	"github.com/mad01/kitty-session/internal/session"
	"github.com/mad01/kitty-session/internal/sidebar"
	"github.com/spf13/cobra"
)

var (
	importDryRun bool
	importFrom   string
	importNoOpen bool
	importTo     string
)

// Import targets for --to.
const (
	targetKs   = "ks"
	targetCmux = "cmux"
)

var importCmd = &cobra.Command{
	Use:   "import",
	Short: "Import herdr's claude agents as ks sessions",
	Long: `Import the claude agents herdr runs into ks, so the next ks brings them back
as sessions. Reads herdr's default session file (~/.config/herdr/session.json),
writes one active ks record per claude pane with its Claude session id, and
attaches unless herdr is still running the agents.

A record that already carries an agent's Claude session id is left alone. A
record for the agent's directory whose id herdr no longer lists gets the new
id instead of a second record, unless its claude window is open in ks.

With --to cmux, opens one cmux workspace per claude agent instead, resuming
its conversation; cmux saves and restores those workspaces itself, so no ks
records are written. Run it from a cmux terminal: cmux only takes commands
from processes it started.`,
	Args: cobra.NoArgs,
	RunE: runImport,
}

func init() {
	importCmd.Flags().BoolVar(&importDryRun, "dry-run", false, "print the plan, write nothing")
	importCmd.Flags().
		StringVar(&importFrom, "from", "", "herdr session.json to read (default: herdr's default session)")
	importCmd.Flags().
		BoolVar(&importNoOpen, "no-open", false, "write the records, do not start the instance")
	importCmd.Flags().
		StringVar(&importTo, "to", targetKs, "where the agents go: ks or cmux")
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
	// actionUpdate gives the record for the agent's directory the agent's
	// Claude session id, in place of one herdr no longer lists.
	actionUpdate
	// actionOpen leaves alone the record actionUpdate would have changed,
	// because its claude window is open: a new id under a running session
	// would make the next resume open another conversation.
	actionOpen
)

// label is the row prefix: in a dry run "import" and "update" say what would
// happen.
func (a importAction) label(dryRun bool) string {
	switch a {
	case actionExists:
		return "exists"
	case actionOpen:
		return "skipped"
	case actionUpdate:
		if dryRun {
			return "update"
		}
		return "updated"
	default:
		if dryRun {
			return "import"
		}
		return "imported"
	}
}

// writes reports whether the action saves a record.
func (a importAction) writes() bool {
	return a == actionImport || a == actionUpdate
}

// importItem is one herdr agent with the ks name it maps to.
type importItem struct {
	action importAction
	name   string
	agent  herdr.Agent
	// record is the existing record the agent maps onto, for actionUpdate
	// and actionOpen; nil for a new one.
	record *session.Session
	// transcriptPath is where Claude Code keeps the agent's conversation;
	// transcriptOK says whether the file is there, so --resume will work.
	transcriptPath string
	transcriptOK   bool
}

// detail is the row's last column: the Claude session id, or the reason a
// record was left alone.
func (it importItem) detail() string {
	if it.action == actionOpen {
		return it.name + " is open in ks, not updated"
	}
	return shortID(it.agent.SessionID)
}

// importCounts is how many agents each action took.
type importCounts struct {
	imports, updates, exists, open int
}

func runImport(cmd *cobra.Command, args []string) error {
	switch importTo {
	case targetKs:
	case targetCmux:
		return runImportToCmux(cmd)
	default:
		return fmt.Errorf("unknown --to %q: want %s or %s", importTo, targetKs, targetCmux)
	}
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

	counts := countActions(items)
	if !importDryRun {
		if err := writeImports(store, items); err != nil {
			return err
		}
	}
	printSummary(cmd.OutOrStdout(), counts, counts.open+len(skipped), importDryRun)
	if importDryRun || counts.imports+counts.updates == 0 {
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

// planAgainstStore opens the store and plans the import against the records
// in it. One snapshot of the instance says which records are open; with the
// instance down, none is.
func planAgainstStore(agents []herdr.Agent) (*session.Store, []importItem, error) {
	w, err := offlineWiring()
	if err != nil {
		return nil, nil, err
	}
	existing, err := w.store.List()
	if err != nil {
		return nil, nil, err
	}
	items, err := planImport(agents, existing, openIn(w.kitty))
	if err != nil {
		return nil, nil, err
	}
	return w.store, items, nil
}

// openIn takes one snapshot of the instance and reports from it whether a
// record's claude window is open. A failed snapshot means the instance is
// down, so no record is.
func openIn(c *kitty.Client) func(*session.Session) bool {
	all, err := c.Windows()
	if err != nil {
		return func(*session.Session) bool { return false }
	}
	return func(s *session.Session) bool {
		_, ok := launcher.ClaudeWindow(all, s)
		return ok
	}
}

// planImport maps every herdr agent to a ks record. The record that already
// carries the agent's Claude session id wins and is left alone. Else the
// record for the agent's directory whose own id herdr no longer lists gets
// the agent's, an active one before a stopped one, unless open says its
// claude window is open. Else a new
// record gets a name free of collisions with the existing records and with
// the other agents of this import.
func planImport(
	agents []herdr.Agent,
	existing []*session.Session,
	open func(*session.Session) bool,
) ([]importItem, error) {
	p := newImportPlan(agents, existing)
	items := make([]importItem, 0, len(agents))
	for _, a := range agents {
		if name, ok := p.byClaudeID[a.SessionID]; ok {
			items = append(items, importItem{action: actionExists, name: name, agent: a})
			continue
		}
		it, err := p.place(a, open)
		if err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	return items, nil
}

// importPlan is what planImport knows about the store while it maps agents:
// the name each Claude session id belongs to, the names in use, and the
// stale records a directory match may still claim.
type importPlan struct {
	byClaudeID map[string]string
	taken      map[string]bool
	// stale holds the records whose Claude session id herdr no longer lists,
	// in candidateOrder. claimStale removes each one it hands out, so an
	// import touches a record at most once.
	stale []*session.Session
}

// newImportPlan indexes the existing records against the agents' ids.
func newImportPlan(agents []herdr.Agent, existing []*session.Session) *importPlan {
	current := make(map[string]bool, len(agents))
	for _, a := range agents {
		current[a.SessionID] = true
	}
	p := &importPlan{byClaudeID: map[string]string{}, taken: map[string]bool{}}
	for _, s := range existing {
		p.taken[s.Name] = true
		if s.ClaudeSessionID != "" {
			p.byClaudeID[s.ClaudeSessionID] = s.Name
		}
		if !current[s.ClaudeSessionID] {
			p.stale = append(p.stale, s)
		}
	}
	slices.SortStableFunc(p.stale, candidateOrder)
	return p
}

// candidateOrder puts active records before stopped ones, then the most
// recently focused first, then the newest created when the focus stamps tie.
func candidateOrder(a, b *session.Session) int {
	if a.IsActive() != b.IsActive() {
		if a.IsActive() {
			return -1
		}
		return 1
	}
	if c := b.FocusedAt.Compare(a.FocusedAt); c != 0 {
		return c
	}
	createdA, _ := time.Parse(time.RFC3339Nano, a.CreatedAt)
	createdB, _ := time.Parse(time.RFC3339Nano, b.CreatedAt)
	return createdB.Compare(createdA)
}

// place maps an agent no record carries the id of: onto the stale record for
// its directory when there is one, else onto a new record.
func (p *importPlan) place(
	a herdr.Agent,
	open func(*session.Session) bool,
) (importItem, error) {
	transcript, ok, err := transcriptFor(a)
	if err != nil {
		return importItem{}, err
	}
	it := importItem{agent: a, transcriptPath: transcript, transcriptOK: ok}
	if rec := p.claimStale(a.Dir); rec != nil {
		it.name, it.record, it.action = rec.Name, rec, actionUpdate
		if open(rec) {
			it.action = actionOpen
		}
	} else {
		it.name, it.action = freeName(importName(a), p.taken), actionImport
		p.taken[it.name] = true
	}
	if it.action.writes() {
		p.byClaudeID[a.SessionID] = it.name
	}
	return it, nil
}

// claimStale hands out the stale record for dir, the most recently focused
// one when several, or nil.
func (p *importPlan) claimStale(dir string) *session.Session {
	for i, s := range p.stale {
		if filepath.Clean(s.Dir) == filepath.Clean(dir) {
			p.stale = slices.Delete(p.stale, i, i+1)
			return s
		}
	}
	return nil
}

// transcriptFor is where Claude Code keeps the agent's conversation, and
// whether the file is still there.
func transcriptFor(a herdr.Agent) (path string, ok bool, err error) {
	path, err = claude.TranscriptPath(a.Dir, a.SessionID)
	if err != nil {
		return "", false, err
	}
	_, statErr := os.Stat(path)
	return path, statErr == nil, nil
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
			sidebar.ShortenHome(it.agent.Dir, home), it.detail())
	}
	for _, s := range skipped {
		fmt.Fprintf(tw, "skipped\t\t%s\t%s\n", sidebar.ShortenHome(s.Dir, home), s.Reason)
	}
	if err := tw.Flush(); err != nil {
		return fmt.Errorf("cannot write import rows: %w", err)
	}
	return nil
}

// transcriptWarnings names the records to write whose transcript is gone.
// The launcher then falls back to --continue or a bare claude, so the
// session comes back without its conversation.
func transcriptWarnings(items []importItem) []error {
	var warnings []error
	for _, it := range items {
		if it.action.writes() && !it.transcriptOK {
			warnings = append(warnings,
				fmt.Errorf("%s: transcript missing, will start fresh", it.name))
		}
	}
	return warnings
}

func countActions(items []importItem) importCounts {
	var c importCounts
	for _, it := range items {
		switch it.action {
		case actionImport:
			c.imports++
		case actionUpdate:
			c.updates++
		case actionExists:
			c.exists++
		case actionOpen:
			c.open++
		}
	}
	return c
}

// printSummary writes the closing count line; skipped counts the herdr panes
// left out together with the records left alone because they are open.
func printSummary(w io.Writer, c importCounts, skipped int, dryRun bool) {
	if dryRun {
		fmt.Fprintf(w,
			"ks: dry run, nothing written: %d to import, %d to update, %d already present, %d skipped\n",
			c.imports, c.updates, c.exists, skipped)
		return
	}
	fmt.Fprintf(w, "ks: %d imported, %d updated, %d already present, %d skipped\n",
		c.imports, c.updates, c.exists, skipped)
}

// writeImports saves an active record with no kitty ids for every item to
// import, which is exactly what attach resumes, and rewrites the record of
// every item to update with the agent's Claude session id and transcript,
// active again if it was stopped: an imported record is there to be resumed.
func writeImports(store *session.Store, items []importItem) error {
	for _, it := range items {
		var sess *session.Session
		switch it.action {
		case actionImport:
			sess = session.New(it.name, it.agent.Dir, 0, 0)
		case actionUpdate:
			sess = it.record
			sess.Status = session.StatusActive
		default:
			continue
		}
		sess.ClaudeSessionID = it.agent.SessionID
		sess.ClaudeTranscriptPath = it.transcriptPath
		if err := store.Save(sess); err != nil {
			return fmt.Errorf("cannot save %s: %w", it.name, err)
		}
	}
	return nil
}

// openImported attaches, bringing the written records up, unless herdr still
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
