package cli

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/mad01/kitty-session/internal/launcher"
	"github.com/mad01/kitty-session/internal/session"
	"github.com/spf13/cobra"
)

var (
	tmpSessionName string
	tmpAgent       string
)

var tmpCmd = &cobra.Command{
	Use:   "tmp",
	Short: "Create a temporary session",
	Long:  "Create a session tab with the agent (claude by default) in a temporary directory.",
	RunE:  runTmp,
}

func init() {
	tmpCmd.Flags().
		StringVarP(&tmpSessionName, "name", "n", "", "session name (auto-generated if omitted)")
	tmpCmd.Flags().StringVar(&tmpAgent, "agent", session.AgentClaude, agentFlagUsage)
	rootCmd.AddCommand(tmpCmd)
}

func runTmp(cmd *cobra.Command, args []string) error {
	kind, err := parseKind(tmpAgent)
	if err != nil {
		return err
	}
	w, err := ensureWiring(false)
	if err != nil {
		return err
	}
	resumeOnFreshStart(cmd, w)

	tmpDir, err := launcher.ScratchDir(w.cfg.EffectiveTmpDir())
	if err != nil {
		return err
	}

	name := tmpSessionName
	auto := name == ""
	if auto {
		name = fmt.Sprintf("tmp-%s", time.Now().Format("0102-1504"))
	}
	req := launcher.Request{Name: name, Dir: tmpDir, Agent: kind}
	res, err := w.launcher.Open(req)
	if auto && errors.Is(err, launcher.ErrExists) {
		// Two tmp sessions in the same minute: disambiguate the generated name.
		name = name + "-" + randomSuffix()
		req.Name = name
		res, err = w.launcher.Open(req)
	}
	if err != nil {
		return withExistsHint(err, name)
	}
	printWarnings(cmd, res.Warnings)

	fmt.Fprintf(cmd.OutOrStdout(), "session %q created in %s\n", name, tmpDir)
	return nil
}

// suffixBytes is the length of the random disambiguator before hex encoding.
const suffixBytes = 2

// randomSuffix returns a short random hex string for a generated name.
func randomSuffix() string {
	b := make([]byte, suffixBytes)
	if _, err := rand.Read(b); err != nil {
		panic("tmp: crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}
