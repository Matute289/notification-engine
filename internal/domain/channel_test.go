package domain

import "testing"

func TestChannel_Valid(t *testing.T) {
	cases := map[Channel]bool{
		ChannelPushIOS:     true,
		ChannelPushAndroid: true,
		ChannelSMS:         true,
		ChannelEmail:       true,
		Channel("fax"):     false,
		Channel(""):        false,
	}
	for ch, want := range cases {
		if got := ch.Valid(); got != want {
			t.Errorf("Channel(%q).Valid() = %v, want %v", ch, got, want)
		}
	}
}

func TestChannel_IsPush(t *testing.T) {
	push := []Channel{ChannelPushIOS, ChannelPushAndroid}
	notPush := []Channel{ChannelSMS, ChannelEmail, Channel("none")}
	for _, c := range push {
		if !c.IsPush() {
			t.Errorf("%q should be push", c)
		}
	}
	for _, c := range notPush {
		if c.IsPush() {
			t.Errorf("%q should not be push", c)
		}
	}
}

func TestParseChannel(t *testing.T) {
	if _, err := ParseChannel("email"); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseChannel("fax"); err == nil {
		t.Fatal("expected error for unknown channel")
	}
}
