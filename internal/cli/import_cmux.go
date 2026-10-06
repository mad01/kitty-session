package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/mad01/kitty-session/internal/claude"
	"github.com/mad01/kitty-session/internal/cmux"
	"github.com/mad01/kitty-session/internal/herdr"
	"github.com/mad01/kitty-session/internal/launcher"
	"github.com/mad01/kitty-session/internal/session"
	"github.com/mad01/kitty-session/internal/sidebar"
	"github.com/spf13/cobra"
)

// workspaceOpener creates cmux workspaces; *cmux.Client in production.
type workspaceOpener interface {
	NewWorkspace(ws cmux.Workspace) error
}

// newWorkspaceOpener is the test seam for the cmux CLI, and insideCmux for
// the check that the CLI will accept the calls.
var (
	newWorkspaceOpener = func() (workspaceOpener, error) { return cmux.New() }
	insideCmux         = cmux.InsideCmux
)

// cmuxItem is one herdr agent planned as a cmux workspace.
type cmuxItem struct {
	ws           cmux.Workspace
	sessionID    string
	transcriptOK bool
}

// runImportToCmux opens one cmux workspace per herdr claude agent, each
// resuming the agent's conversation the way a ks relaunch would.
func runImportToCmux(cmd *cobra.Command) error {
	path, snap, err := loadHerdrSnapshot()
	if err != nil {
		return err
	}
	agents, skipped := snap.Agents()
	if len(agents) == 0 && len(skipped) == 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "ks: no agents found in %s\n", path)
		return nil
	}
	items, err := planCmux(agents)
	if err != nil {
		return err
	}
	if err := printCmuxRows(cmd.OutOrStdout(), items, skipped, importDryRun); err != nil {
		return err
	}
	printWarnings(cmd, cmuxTranscriptWarnings(items))
	if importDryRun {
		fmt.Fprintf(cmd.OutOrStdout(), "ks: dry run, nothing opened: %d to open, %d skipped\n",
			len(items), len(skipped))
		return nil
	}
	if herdr.Running(filepath.Dir(path)) {
		fmt.Fprintln(cmd.ErrOrStderr(),
			"ks: herdr is still running these agents; not opening them.")
		fmt.Fprintln(cmd.ErrOrStderr(),
			"    Stop it with: herdr session stop default   then rerun this import")
		return nil
	}
	if !insideCmux() {
		return errors.New(
			"cmux only takes commands from its own terminals; run ks import --to cmux inside cmux")
	}
	opener, err := newWorkspaceOpener()
	if err != nil {
		return err
	}
	opened, err := openCmuxWorkspaces(opener, items)
	fmt.Fprintf(cmd.OutOrStdout(), "ks: %d opened in cmux, %d skipped\n", opened, len(skipped))
	return err
}

// planCmux maps every agent to a workspace named the way ks import names
// records, unique within this import, running the same claude command a ks
// relaunch would: --resume while the transcript exists, else the fallbacks.
func planCmux(agents []herdr.Agent) ([]cmuxItem, error) {
	taken := map[string]bool{}
	items := make([]cmuxItem, 0, len(agents))
	for _, a := range agents {
		name := freeName(importName(a), taken)
		taken[name] = true
		transcript, err := claude.TranscriptPath(a.Dir, a.SessionID)
		if err != nil {
			return nil, err
		}
		_, statErr := os.Stat(transcript)
		sess := session.New(name, a.Dir, 0, 0)
		sess.ClaudeSessionID = a.SessionID
		sess.ClaudeTranscriptPath = transcript
		items = append(items, cmuxItem{
			ws: cmux.Workspace{
				Name:    name,
				Dir:     a.Dir,
				Command: launcher.ClaudeCmd(sess, launcher.ResumeStored),
			},
			sessionID:    a.SessionID,
			transcriptOK: statErr == nil,
		})
	}
	return items, nil
}

// openCmuxWorkspaces creates every planned workspace, carrying on past a
// failure so one bad directory does not strand the rest. It returns how many
// it opened and the failures joined.
func openCmuxWorkspaces(opener workspaceOpener, items []cmuxItem) (int, error) {
	opened := 0
	var errs []error
	for _, it := range items {
		if err := opener.NewWorkspace(it.ws); err != nil {
			errs = append(errs, err)
			continue
		}
		opened++
	}
	return opened, errors.Join(errs...)
}

// printCmuxRows writes one aligned line per workspace with the command it
// runs, then one per skipped pane with its reason.
func printCmuxRows(w io.Writer, items []cmuxItem, skipped []herdr.Skip, dryRun bool) error {
	label := "open"
	if !dryRun {
		label = "opening"
	}
	home, _ := os.UserHomeDir()
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, it := range items {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", label, it.ws.Name,
			sidebar.ShortenHome(it.ws.Dir, home), cmux.ShellJoin(it.ws.Command))
	}
	for _, s := range skipped {
		fmt.Fprintf(tw, "skipped\t\t%s\t%s\n", sidebar.ShortenHome(s.Dir, home), s.Reason)
	}
	if err := tw.Flush(); err != nil {
		return fmt.Errorf("cannot write import rows: %w", err)
	}
	return nil
}

// cmuxTranscriptWarnings names the agents whose transcript is gone, which
// therefore come back without their conversation.
func cmuxTranscriptWarnings(items []cmuxItem) []error {
	var warnings []error
	for _, it := range items {
		if !it.transcriptOK {
			warnings = append(
				warnings,
				fmt.Errorf("%s: transcript for %s missing, will not resume it",
					it.ws.Name, shortID(it.sessionID)),
			)
		}
	}
	return warnings
}
