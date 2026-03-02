package session

import "time"

type Session struct {
	Name       string `json:"name"`
	Dir        string `json:"dir"`
	CreatedAt  string `json:"created_at"`
	KittyTabID int    `json:"kitty_tab_id"`
}

func New(name, dir string, tabID int) *Session {
	return &Session{
		Name:       name,
		Dir:        dir,
		CreatedAt:  time.Now().UTC().Format(time.RFC3339),
		KittyTabID: tabID,
	}
}
