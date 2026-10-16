package cli

import (
	"github.com/mad01/kitty-session/internal/sidebar"
	"github.com/spf13/cobra"
)

var (
	sidebarDemoWidth   int
	sidebarDemoSession string
)

// sidebarDemoCmd runs the sidebar against fake agents so its look can be
// reviewed in any terminal. Hidden: it is a development aid, not a feature.
var sidebarDemoCmd = &cobra.Command{
	Use:    "_sidebar-demo",
	Short:  "Run the sidebar UI on fake data (no kitty needed)",
	Hidden: true,
	Args:   cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		backend := sidebar.NewDemoBackend()
		backend.SetOwn(sidebarDemoSession)
		// Both notifications are on, so a kitty user can preview the
		// banner: --session names a row at input or done, and the first
		// refresh raises it.
		return sidebar.Run(sidebar.Options{
			Session:   sidebarDemoSession,
			SessionID: "demo",
			Width:     sidebarDemoWidth,
			Backend:   backend,
			Notify:    []string{sidebar.StateInput.String(), sidebar.StateDone.String()},
		})
	},
}

func init() {
	sidebarDemoCmd.Flags().
		IntVar(&sidebarDemoWidth, "width", sidebar.DefaultWidth, "frame width in columns")
	sidebarDemoCmd.Flags().
		StringVar(&sidebarDemoSession, "session", "kitty-session",
			"name of the fake agent treated as this tab's own")
	rootCmd.AddCommand(sidebarDemoCmd)
}
