package kitty

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
)

// Tab represents a kitty tab from `kitty @ ls` output.
type Tab struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
}

// Window represents a kitty OS window from `kitty @ ls` output.
type Window struct {
	Tabs []Tab `json:"tabs"`
}

// LaunchTab creates a new kitty OS window running the given command in dir.
// Returns the new window's ID.
func LaunchTab(dir string, args ...string) (int, error) {
	cmdArgs := []string{"@", "launch", "--type=os-window", "--cwd=" + dir}
	cmdArgs = append(cmdArgs, args...)
	out, err := exec.Command("kitty", cmdArgs...).Output()
	if err != nil {
		return 0, fmt.Errorf("kitty @ launch tab: %w", err)
	}
	// kitty @ launch prints the new window id
	id, err := strconv.Atoi(trimOutput(out))
	if err != nil {
		return 0, fmt.Errorf("cannot parse window id: %w (output: %q)", err, string(out))
	}
	return id, nil
}

// LaunchSplit creates a horizontal split in the current tab running in dir.
func LaunchSplit(dir string, args ...string) error {
	cmdArgs := []string{"@", "launch", "--type=window", "--location=hsplit", "--bias=30", "--cwd=" + dir}
	cmdArgs = append(cmdArgs, args...)
	if err := exec.Command("kitty", cmdArgs...).Run(); err != nil {
		return fmt.Errorf("kitty @ launch split: %w", err)
	}
	return nil
}

// SetTabTitle sets the title of the tab containing the given window.
func SetTabTitle(title string) error {
	if err := exec.Command("kitty", "@", "set-tab-title", title).Run(); err != nil {
		return fmt.Errorf("kitty @ set-tab-title: %w", err)
	}
	return nil
}

// FocusTab focuses a tab by its ID.
func FocusTab(tabID int) error {
	if err := exec.Command("kitty", "@", "focus-tab", "--match=id:"+strconv.Itoa(tabID)).Run(); err != nil {
		return fmt.Errorf("kitty @ focus-tab: %w", err)
	}
	return nil
}

// CloseTab closes a tab by its ID.
func CloseTab(tabID int) error {
	if err := exec.Command("kitty", "@", "close-tab", "--match=id:"+strconv.Itoa(tabID)).Run(); err != nil {
		return fmt.Errorf("kitty @ close-tab: %w", err)
	}
	return nil
}

// ListTabs returns all tabs across all kitty OS windows.
func ListTabs() ([]Tab, error) {
	out, err := exec.Command("kitty", "@", "ls").Output()
	if err != nil {
		return nil, fmt.Errorf("kitty @ ls: %w", err)
	}
	var windows []Window
	if err := json.Unmarshal(out, &windows); err != nil {
		return nil, fmt.Errorf("cannot parse kitty @ ls output: %w", err)
	}
	var tabs []Tab
	for _, w := range windows {
		tabs = append(tabs, w.Tabs...)
	}
	return tabs, nil
}

// TabExists checks whether a tab with the given ID is still alive.
func TabExists(tabID int) bool {
	tabs, err := ListTabs()
	if err != nil {
		return false
	}
	for _, t := range tabs {
		if t.ID == tabID {
			return true
		}
	}
	return false
}

// DetailedTab includes window information for tab lookups.
type DetailedTab struct {
	ID      int              `json:"id"`
	Title   string           `json:"title"`
	Windows []DetailedWindow `json:"windows"`
}

// DetailedWindow represents a window (pane) inside a tab.
type DetailedWindow struct {
	ID int `json:"id"`
}

// DetailedOSWindow is an OS window containing detailed tabs.
type DetailedOSWindow struct {
	Tabs []DetailedTab `json:"tabs"`
}

// ListTabsDetailed returns all tabs with their window IDs.
func ListTabsDetailed() ([]DetailedTab, error) {
	out, err := exec.Command("kitty", "@", "ls").Output()
	if err != nil {
		return nil, fmt.Errorf("kitty @ ls: %w", err)
	}
	var osWindows []DetailedOSWindow
	if err := json.Unmarshal(out, &osWindows); err != nil {
		return nil, fmt.Errorf("cannot parse kitty @ ls output: %w", err)
	}
	var tabs []DetailedTab
	for _, w := range osWindows {
		tabs = append(tabs, w.Tabs...)
	}
	return tabs, nil
}

// FocusWindow focuses a specific window (pane) by ID.
func FocusWindow(windowID int) error {
	if err := exec.Command("kitty", "@", "focus-window", "--match=id:"+strconv.Itoa(windowID)).Run(); err != nil {
		return fmt.Errorf("kitty @ focus-window: %w", err)
	}
	return nil
}

// FindTabForWindow returns the tab ID that contains the given window (pane) ID.
func FindTabForWindow(windowID int) (int, error) {
	tabs, err := ListTabsDetailed()
	if err != nil {
		return 0, err
	}
	for _, t := range tabs {
		for _, w := range t.Windows {
			if w.ID == windowID {
				return t.ID, nil
			}
		}
	}
	return 0, fmt.Errorf("no tab found containing window %d", windowID)
}

// GetText reads the terminal text from a specific window (pane) by ID.
func GetText(windowID int) (string, error) {
	out, err := exec.Command("kitty", "@", "get-text", "--match=id:"+strconv.Itoa(windowID)).Output()
	if err != nil {
		return "", fmt.Errorf("kitty @ get-text: %w", err)
	}
	return string(out), nil
}

// FirstWindowInTab returns the first window (pane) ID inside the given tab.
func FirstWindowInTab(tabID int) (int, error) {
	tabs, err := ListTabsDetailed()
	if err != nil {
		return 0, err
	}
	for _, t := range tabs {
		if t.ID == tabID {
			if len(t.Windows) == 0 {
				return 0, fmt.Errorf("tab %d has no windows", tabID)
			}
			return t.Windows[0].ID, nil
		}
	}
	return 0, fmt.Errorf("tab %d not found", tabID)
}

func trimOutput(b []byte) string {
	s := string(b)
	// Trim whitespace and newlines
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r' || s[len(s)-1] == ' ') {
		s = s[:len(s)-1]
	}
	return s
}
