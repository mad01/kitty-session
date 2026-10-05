// Package procinfo answers two questions about a live process: who its parent
// is and what its executable is called. The ks hook uses them to tell the
// Claude that ks launched (a direct child of kitty) apart from a Claude nested
// inside a session, which inherits the same environment.
//
// Only darwin has an implementation; elsewhere both lookups return
// ErrUnsupported and callers fall back to trusting the environment.
package procinfo

import "errors"

// ErrUnsupported is returned on platforms without a process lookup.
var ErrUnsupported = errors.New("procinfo: unsupported on this platform")
