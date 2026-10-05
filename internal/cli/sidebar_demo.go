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
		return sidebar.Run(sidebar.Options{
			Session: sidebarDemoSession,
			Width:   sidebarDemoWidth,
			Backend: backend,
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
