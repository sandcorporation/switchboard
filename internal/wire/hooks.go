package wire

import (
	"fmt"
	"strings"
)

// Incoming webhooks (#527): a tool's Discord- or Slack-style notice posted
// to POST /hook/<value> becomes alert mail to the hook's role; in forward
// mode the server also sends the body on to the original destination.
const (
	HookModeMail    = "mail"    // the tool posts to the destination itself; switchboard only mails
	HookModeForward = "forward" // switchboard receives and sends the body on

	HookDestNone       = "none"       // no original destination
	HookDestSet        = "set"        // one is stored (never shown again)
	HookDestUnreadable = "unreadable" // stored under another key: register it again

	HookForwardNone    = ""        // nothing to send on
	HookForwardPending = "pending" // waiting for a (next) try
	HookForwardSent    = "sent"
	HookForwardFailed  = "failed"

	HookWindowDefault = 10   // minutes
	HookWindowMax     = 1440 // minutes
	HookNameMax       = 64
)

// Hook is one webhook as the dashboard lists it. Neither the address value
// nor the original destination is ever in it: Dest says only whether one is
// stored.
type Hook struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Role      string `json:"role"`
	Mode      string `json:"mode"`
	Urgent    bool   `json:"urgent"`
	WindowMin int    `json:"window_min"`
	Dest      string `json:"dest"`
	DestAt    string `json:"dest_at,omitempty"`
	LastAt    string `json:"last_at,omitempty"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// HookReceipt is one received notice: when, its subject and what became of
// sending it on (Forward, the destination's last status code, tries).
type HookReceipt struct {
	At       string `json:"at"`
	Subject  string `json:"subject"`
	Forward  string `json:"forward"`
	Status   int    `json:"status,omitempty"`
	Attempts int    `json:"attempts,omitempty"`
}

// HookDetail is one hook with its recent receipts, newest first.
type HookDetail struct {
	Hook
	Receipts []HookReceipt `json:"receipts"`
}

// Webhook admin action kinds (#549), the action of an AdminAction row: the
// target is hook:<name>, the detail never a value.
const (
	ActionHookAdd     = "hook.add"
	ActionHookChange  = "hook.change"
	ActionHookRemove  = "hook.remove"
	ActionHookAddress = "hook.address"
)

// HookCreated answers adding a hook or making its address again: Token is
// the address value (POST /hook/<token>), shown this once only.
type HookCreated struct {
	Hook
	Token string `json:"token"`
}

// HookSummary is what a role sees (`switchboard hooks list`).
type HookSummary struct {
	Name   string `json:"name"`
	Role   string `json:"role"`
	Mode   string `json:"mode"`
	LastAt string `json:"last_at,omitempty"`
}

// HookSave adds or changes a hook. Dest, when not empty, replaces the
// original destination; ClearDest removes it.
type HookSave struct {
	Name      string `json:"name"`
	Role      string `json:"role"`
	Mode      string `json:"mode"`
	Urgent    bool   `json:"urgent"`
	WindowMin int    `json:"window_min"`
	Dest      string `json:"dest,omitempty"`
	ClearDest bool   `json:"clear_dest,omitempty"`
}

// Check validates the fields and fills the default window.
func (h *HookSave) Check() error {
	h.Name = strings.TrimSpace(h.Name)
	h.Role = strings.TrimSpace(h.Role)
	h.Dest = strings.TrimSpace(h.Dest)
	if h.Name == "" || len(h.Name) > HookNameMax || strings.ContainsAny(h.Name, "\r\n\t") {
		return fmt.Errorf("name must be 1-%d characters on one line", HookNameMax)
	}
	if !ValidRole(h.Role) {
		return fmt.Errorf("role %q is not a role name", h.Role)
	}
	if h.Mode != HookModeMail && h.Mode != HookModeForward {
		return fmt.Errorf("mode must be %s or %s", HookModeMail, HookModeForward)
	}
	if h.WindowMin == 0 {
		h.WindowMin = HookWindowDefault
	}
	if h.WindowMin < 1 || h.WindowMin > HookWindowMax {
		return fmt.Errorf("window_min must be 1-%d", HookWindowMax)
	}
	if h.Dest != "" && h.ClearDest {
		return fmt.Errorf("dest and clear_dest together")
	}
	if h.Dest != "" {
		if len(h.Dest) > 2048 || !(strings.HasPrefix(h.Dest, "https://") || strings.HasPrefix(h.Dest, "http://")) || strings.ContainsAny(h.Dest, " \r\n\t") {
			return fmt.Errorf("dest must be an http(s) address up to 2048 characters")
		}
	}
	return nil
}
