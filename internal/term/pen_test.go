package term

import "testing"

// A screen snapshot must leave the viewer's pen where the program left it:
// ConPTY sends only attribute changes, so output that follows the snapshot
// assumes the reverse video and colours it set before still apply.
func TestSnapshotKeepsPenForFollowingOutput(t *testing.T) {
	const cols, rows = 20, 4
	// Child output as ConPTY writes it: reverse and a colour set once, then
	// text written without repeating them.
	before := "\x1b[?25l\x1b[2J\x1b[m\x1b[H\x1b[7m\x1b[41mbanner\r\n"
	after := "next"

	live := NewEmulator(cols, rows, 0)
	defer live.Close()
	live.Write([]byte(before))
	snap := live.Grid(-1).Snapshot()
	live.Write([]byte(after))

	viewer := NewEmulator(cols, rows, 0)
	defer viewer.Close()
	viewer.Write(snap)
	viewer.Write([]byte(after))

	want, got := live.Grid(-1), viewer.Grid(-1)
	if want.Screen[1][0].Attr != got.Screen[1][0].Attr {
		t.Fatalf("text after the snapshot: attr %v, want %v", got.Screen[1][0].Attr, want.Screen[1][0].Attr)
	}
	if !want.Screen[1][0].Attr.Reverse {
		t.Fatalf("scene did not leave reverse on: %v", want.Screen[1][0].Attr)
	}
}
