package cli

import (
	"errors"
	"io/fs"

	"github.com/mad01/kitty-session/internal/instance"
	"github.com/mad01/kitty-session/internal/kitty"
	"github.com/mad01/kitty-session/internal/launcher"
	"github.com/mad01/kitty-session/internal/repo/config"
	"github.com/mad01/kitty-session/internal/session"
)

// wiring is what a session command works with: the store, the config (nil
// when the file is absent), the client for the ks instance, and a launcher
// bound to it. The client is shared, so a command takes one snapshot rather
// than building a second client of its own.
type wiring struct {
	store    *session.Store
	cfg      *config.Config
	kitty    *kitty.Client
	launcher *launcher.Launcher
	// started is true when ensureWiring had to start the instance. new, open
	// and tmp then bring the active sessions back before adding their own tab.
	started bool
}

// loadConfig reads config.yaml, treating a missing file as no config and any
// other problem as an error.
func loadConfig() (*config.Config, error) {
	cfg, err := config.Load()
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	return cfg, nil
}

// ensureWiring starts the instance when needed. Commands that create or
// focus tabs use it.
func ensureWiring(agent bool) (wiring, error) {
	store, cfg, err := storeAndConfig()
	if err != nil {
		return wiring{}, err
	}
	c, started, err := instance.Ensure(cfg, instance.Options{Agent: agent})
	if err != nil {
		return wiring{}, err
	}
	l, err := launcher.New(store, c, cfg)
	if err != nil {
		return wiring{}, err
	}
	return wiring{store: store, cfg: cfg, kitty: c, launcher: l, started: started}, nil
}

// offlineWiring never starts the instance. Commands that must work while it
// is down (close, rename) use it; their kitty calls then fail fast and
// surface as warnings.
func offlineWiring() (wiring, error) {
	store, cfg, err := storeAndConfig()
	if err != nil {
		return wiring{}, err
	}
	c, err := instance.Client(cfg)
	if err != nil {
		return wiring{}, err
	}
	l, err := launcher.New(store, c, cfg)
	if err != nil {
		return wiring{}, err
	}
	return wiring{store: store, cfg: cfg, kitty: c, launcher: l}, nil
}

func storeAndConfig() (*session.Store, *config.Config, error) {
	store, err := session.NewStore()
	if err != nil {
		return nil, nil, err
	}
	cfg, err := loadConfig()
	if err != nil {
		return nil, nil, err
	}
	return store, cfg, nil
}
