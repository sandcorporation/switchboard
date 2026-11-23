package wire

// Per-login permissions on the work screen (#476): what a dashboard login
// may do. adminauth keeps them; the server and hosting check them.
const (
	PermView     = "view"     // terminals and files, read only
	PermType     = "type"     // input into a terminal
	PermFiles    = "files"    // save, delete, rename and upload files
	PermSessions = "sessions" // start, stop, restart, change or delete a hosted session
	PermDiscard  = "discard"  // drop an item of the left ledger
)

// AllPerms is every permission, in the order lists show them.
var AllPerms = []string{PermView, PermType, PermFiles, PermSessions, PermDiscard}
