package auth

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func fixedClock(ts int64) func() time.Time {
	return func() time.Time { return time.Unix(ts, 0) }
}

func TestVerify_RoundTrip(t *testing.T) {
	v := NewVerifier(map[string]string{"app": "secret"}, time.Minute)
	v.SetClock(fixedClock(1700000000))

	method, path, body := "POST", "/v1/notifications", []byte(`{"hi":1}`)
	ts := "1700000000"
	sig := Sign("secret", ts, method, path, "", body)
	got, err := v.Verify("app", ts, sig, method, path, "", body)
	require.NoError(t, err)
	require.Equal(t, "app", got)
}

func TestVerify_BadSecret(t *testing.T) {
	v := NewVerifier(map[string]string{"app": "secret"}, time.Minute)
	v.SetClock(fixedClock(1700000000))
	sig := Sign("wrong", "1700000000", "POST", "/x", "", []byte("{}"))
	_, err := v.Verify("app", "1700000000", sig, "POST", "/x", "", []byte("{}"))
	require.Error(t, err)
}

func TestVerify_StaleTimestamp(t *testing.T) {
	v := NewVerifier(map[string]string{"app": "secret"}, time.Minute)
	v.SetClock(fixedClock(1700000000))
	sig := Sign("secret", "1699990000", "POST", "/x", "", []byte("{}"))
	_, err := v.Verify("app", "1699990000", sig, "POST", "/x", "", []byte("{}"))
	require.Error(t, err)
}

func TestVerify_UnknownKey(t *testing.T) {
	v := NewVerifier(map[string]string{"app": "secret"}, time.Minute)
	v.SetClock(fixedClock(1700000000))
	sig := Sign("secret", "1700000000", "POST", "/x", "", []byte("{}"))
	_, err := v.Verify("nope", "1700000000", sig, "POST", "/x", "", []byte("{}"))
	require.Error(t, err)
}

func TestVerify_RoundTrip_WithOnBehalfOf(t *testing.T) {
	v := NewVerifier(map[string]string{"app": "secret"}, time.Minute)
	v.SetClock(fixedClock(1700000000))

	method, path, body := "POST", "/v1/users/42/devices", []byte(`{"device_token":"tok","channel":"push_ios"}`)
	ts := "1700000000"
	sig := Sign("secret", ts, method, path, "42", body)
	got, err := v.Verify("app", ts, sig, method, path, "42", body)
	require.NoError(t, err)
	require.Equal(t, "app", got)
}

func TestVerify_RoundTrip_EmptyOnBehalfOf(t *testing.T) {
	v := NewVerifier(map[string]string{"app": "secret"}, time.Minute)
	v.SetClock(fixedClock(1700000000))

	method, path, body := "POST", "/v1/notifications", []byte(`{"hi":1}`)
	ts := "1700000000"
	sig := Sign("secret", ts, method, path, "", body)
	got, err := v.Verify("app", ts, sig, method, path, "", body)
	require.NoError(t, err)
	require.Equal(t, "app", got)
}

func TestVerify_TamperedOnBehalfOf_Fails(t *testing.T) {
	v := NewVerifier(map[string]string{"app": "secret"}, time.Minute)
	v.SetClock(fixedClock(1700000000))

	method, path, body := "POST", "/v1/users/42/devices", []byte(`{"device_token":"tok","channel":"push_ios"}`)
	ts := "1700000000"
	// Sign with onBehalfOf="42" but verify with "99" → must fail.
	sig := Sign("secret", ts, method, path, "42", body)
	_, err := v.Verify("app", ts, sig, method, path, "99", body)
	require.Error(t, err)
}
