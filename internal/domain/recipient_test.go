package domain

import (
	"errors"
	"testing"
)

func TestParseEmail(t *testing.T) {
	good := []string{"a@b.com", "X.Y@example.co.uk", ""}
	bad := []string{"not-an-email", "@x", "x@"}
	for _, s := range good {
		if _, err := ParseEmail(s); err != nil {
			t.Errorf("ParseEmail(%q) returned error: %v", s, err)
		}
	}
	for _, s := range bad {
		if _, err := ParseEmail(s); err == nil {
			t.Errorf("ParseEmail(%q) should have failed", s)
		}
	}
}

func TestParsePhone(t *testing.T) {
	good := []string{"+15551234567", "5551234567", ""}
	bad := []string{"abc", "12-34-56", "++15551234567", "1"}
	for _, s := range good {
		if _, err := ParsePhone(s); err != nil {
			t.Errorf("ParsePhone(%q) returned error: %v", s, err)
		}
	}
	for _, s := range bad {
		if _, err := ParsePhone(s); err == nil {
			t.Errorf("ParsePhone(%q) should have failed", s)
		}
	}
}

func TestRecipient_Validate(t *testing.T) {
	uid := int64(1)
	cases := []struct {
		name    string
		ch      Channel
		r       Recipient
		wantErr bool
	}{
		{"empty all", ChannelEmail, Recipient{}, true},
		{"user only OK regardless of channel", ChannelEmail, Recipient{UserID: &uid}, false},
		{"email channel needs email", ChannelEmail, Recipient{Email: ""}, true},
		{"email channel happy", ChannelEmail, Recipient{Email: "a@b.com"}, false},
		{"sms needs phone", ChannelSMS, Recipient{Phone: ""}, true},
		{"sms happy", ChannelSMS, Recipient{Phone: "+15551234567"}, false},
		{"push needs token", ChannelPushIOS, Recipient{DeviceToken: ""}, true},
		{"push happy", ChannelPushIOS, Recipient{DeviceToken: "abc"}, false},
		{"whatsapp needs phone", ChannelWhatsApp, Recipient{}, true},
		{"whatsapp happy", ChannelWhatsApp, Recipient{Phone: "+15551234567"}, false},
		{"telegram needs messaging_id", ChannelTelegram, Recipient{}, true},
		{"telegram happy", ChannelTelegram, Recipient{MessagingID: "123456789"}, false},
		{"line needs messaging_id", ChannelLine, Recipient{}, true},
		{"line happy", ChannelLine, Recipient{MessagingID: "U1234567890"}, false},
		{"fbmessenger needs messaging_id", ChannelFacebookMessenger, Recipient{}, true},
		{"fbmessenger happy", ChannelFacebookMessenger, Recipient{MessagingID: "987654321"}, false},
	}
	for _, c := range cases {
		err := c.r.Validate(c.ch)
		if (err != nil) != c.wantErr {
			t.Errorf("%s: err=%v wantErr=%v", c.name, err, c.wantErr)
		}
		if err != nil && !errors.Is(err, ErrInvalidInput) {
			t.Errorf("%s: error should wrap ErrInvalidInput, got %v", c.name, err)
		}
	}
}

func TestParseEventID(t *testing.T) {
	if _, err := ParseEventID(""); err == nil {
		t.Fatal("empty event id should fail")
	}
	if _, err := ParseEventID("abc"); err != nil {
		t.Fatal(err)
	}
}
