package channel

import "testing"

func TestShouldProcess(t *testing.T) {
	in := &Inbound{MessageKind: "text", Text: "hi", IsGroup: false}
	if !ShouldProcess(in) {
		t.Fatal("p2p text should process")
	}
	in.IsGroup = true
	in.MentionBot = false
	if ShouldProcess(in) {
		t.Fatal("group without mention should skip")
	}
}
