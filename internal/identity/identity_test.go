package identity

import "testing"

func TestSystemSenderReserved(t *testing.T) {
	for _, n := range []string{"switchboard/system", "switchboard/system-2"} {
		if !Reserved(n) {
			t.Errorf("%s not reserved", n)
		}
	}
	if !IsSystemSender(SystemSender) || IsSystemSender("team/system") {
		t.Fatal("isSystemSender")
	}
	if SystemSender != "switchboard/system" {
		t.Fatalf("new mail must be sent as switchboard/system, got %s", SystemSender)
	}
}
