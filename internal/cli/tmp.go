package cli

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/mad01/kitty-session/internal/launcher"
	"github.com/spf13/cobra"
)

var tmpSessionName string

var tmpCmd = &cobra.Command{
	Use:   "tmp",
	Short: "Create a temporary Claude session",
	Long:  "Create a session tab with claude in a temporary directory.",
	RunE:  runTmp,
}

func init() {
	tmpCmd.Flags().
		StringVarP(&tmpSessionName, "name", "n", "", "session name (auto-generated if omitted)")
	rootCmd.AddCommand(tmpCmd)
}

func runTmp(cmd *cobra.Command, args []string) error {
	w, err := ensureWiring(false)
	if err != nil {
		return err
	}

	tmpBase := w.cfg.EffectiveTmpDir()
	if tmpBase != "" {
		if err := os.MkdirAll(tmpBase, 0o755); err != nil {
			return fmt.Errorf("cannot create tmpdir: %w", err)
		}
	}
	tmpDir, err := os.MkdirTemp(tmpBase, "ks-*")
	if err != nil {
		return fmt.Errorf("cannot create temp directory: %w", err)
	}

	name := tmpSessionName
	auto := name == ""
	if auto {
		name = fmt.Sprintf("tmp-%s", time.Now().Format("0102-1504"))
	}
	res, err := w.launcher.Open(launcher.Request{Name: name, Dir: tmpDir})
	if auto && errors.Is(err, launcher.ErrExists) {
		// Two tmp sessions in the same minute: disambiguate the generated name.
		name = name + "-" + randomSuffix()
		res, err = w.launcher.Open(launcher.Request{Name: name, Dir: tmpDir})
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
