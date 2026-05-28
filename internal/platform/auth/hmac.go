// Package auth implements the HMAC scheme that protects every public API call.
//
// Clients sign each request with a shared secret. The signature covers
// timestamp + method + path + raw body, so replays outside a configurable skew
// window are rejected. Secrets are matched in constant time.
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"time"
)

const (
	HeaderAppKey    = "X-App-Key"
	HeaderTimestamp = "X-App-Timestamp"
	HeaderSignature = "X-App-Signature"
)

// Verifier verifies the HMAC headers attached to incoming requests.
type Verifier struct {
	clients map[string]string
	skew    time.Duration
	now     func() time.Time
}

func NewVerifier(clients map[string]string, skew time.Duration) *Verifier {
	return &Verifier{clients: clients, skew: skew, now: time.Now}
}

// Verify checks that key/signature/timestamp are valid for (method, path, onBehalfOf, body).
// It returns the AppKey on success so handlers can attribute the request.
// onBehalfOf may be empty for clients that do not send X-On-Behalf-Of-User.
func (v *Verifier) Verify(key, timestamp, signature, method, path, onBehalfOf string, body []byte) (string, error) {
	if key == "" || timestamp == "" || signature == "" {
		return "", errors.New("missing auth headers")
	}
	secret, ok := v.clients[key]
	if !ok {
		return "", errors.New("unknown app key")
	}
	tsUnix, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return "", fmt.Errorf("malformed timestamp: %w", err)
	}
	delta := v.now().Sub(time.Unix(tsUnix, 0))
	if delta < -v.skew || delta > v.skew {
		return "", errors.New("timestamp outside allowed skew window")
	}
	expected := Sign(secret, timestamp, method, path, onBehalfOf, body)
	if !hmac.Equal([]byte(expected), []byte(signature)) {
		return "", errors.New("signature mismatch")
	}
	return key, nil
}

// Sign produces the canonical signature for the given inputs.
// onBehalfOf may be empty string; when empty it is still included as a blank
// line so the canonical string remains unambiguous.
func Sign(secret, timestamp, method, path, onBehalfOf string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("\n"))
	mac.Write([]byte(method))
	mac.Write([]byte("\n"))
	mac.Write([]byte(path))
	mac.Write([]byte("\n"))
	mac.Write([]byte(onBehalfOf))
	mac.Write([]byte("\n"))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// SetClock is a test-only seam used by hmac_test.go.
func (v *Verifier) SetClock(now func() time.Time) { v.now = now }
