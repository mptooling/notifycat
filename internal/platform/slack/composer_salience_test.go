package slack

import "testing"

func TestStuckDigestListAttentionAndNote(t *testing.T) {
	composer := NewComposer("eyes")
	msg := composer.StuckDigestList([]StuckPR{
		{Repository: "acme/api", Number: 7, URL: "https://github.com/acme/api/pull/7", IdleDays: 3, Attention: true, Note: "blocks the release"},
		{Repository: "acme/web", Number: 9, URL: "https://github.com/acme/web/pull/9", IdleDays: 1},
	})
	text := msg.Blocks[0].Text.Text
	wantFirst := "• :warning: <https://github.com/acme/api/pull/7|acme/api #7> · idle 3 days — _blocks the release_"
	wantSecond := "• <https://github.com/acme/web/pull/9|acme/web #9> · idle 1 day"
	if text != wantFirst+"\n"+wantSecond {
		t.Errorf("list = %q\nwant %q", text, wantFirst+"\n"+wantSecond)
	}
}
