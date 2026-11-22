package conpty

import "time"

// SetAcceptPause sets the wait between failed accepts and returns the old one.
func SetAcceptPause(d time.Duration) time.Duration {
	old := acceptPause
	acceptPause = d
	return old
}
