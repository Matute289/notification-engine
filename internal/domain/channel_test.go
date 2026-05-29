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

func TestChannel_Valid_SocialChannels(t *testing.T) {
	cases := []struct {
		ch   Channel
		want bool
	}{
		{ChannelTelegram, true},
		{ChannelWhatsApp, true},
		{ChannelLine, true},
		{ChannelFacebookMessenger, true},
		{Channel("discord"), false},
	}
	for _, c := range cases {
		if got := c.ch.Valid(); got != c.want {
			t.Errorf("Channel(%q).Valid() = %v, want %v", c.ch, got, c.want)
		}
	}
}

func TestParseChannel_SocialChannels(t *testing.T) {
	for _, s := range []string{"telegram", "whatsapp", "line", "facebook_messenger"} {
		ch, err := ParseChannel(s)
		if err != nil {
			t.Errorf("ParseChannel(%q) unexpected error: %v", s, err)
		}
		if string(ch) != s {
			t.Errorf("ParseChannel(%q) = %q, want %q", s, ch, s)
		}
	}
}

func TestAllChannels_IncludesSocialChannels(t *testing.T) {
	all := AllChannels()
	want := map[Channel]bool{
		ChannelTelegram: true, ChannelWhatsApp: true,
		ChannelLine: true, ChannelFacebookMessenger: true,
	}
	found := map[Channel]bool{}
	for _, ch := range all {
		found[ch] = true
	}
	for ch := range want {
		if !found[ch] {
			t.Errorf("AllChannels() missing %q", ch)
		}
	}
}
