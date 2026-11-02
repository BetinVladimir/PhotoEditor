package pagination

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"photoeditor/backend/internal/auth"
)

var testKey32 = bytes.Repeat([]byte{0x77}, 32)

func validCodec() Codec {
	now := time.Unix(1_700_000_000, 0)
	return Codec{Key: testKey32, Now: func() time.Time { return now }, TTL: time.Hour}
}

func validScope() Cursor {
	return Cursor{Tenant: "t", Principal: "p", FilterHash: "h", Sort: "s"}
}

func TestCodecNowFallsBackToRealTime(t *testing.T) {
	c := Codec{Key: testKey32} // no Now set
	got := c.now()
	if got.IsZero() {
		t.Fatal("now() with nil Now must return real time, got zero")
	}
}

func TestEncodeRejectsShortKey(t *testing.T) {
	c := Codec{Key: bytes.Repeat([]byte{1}, 16)}
	if _, err := c.Encode(validScope()); err == nil {
		t.Fatal("Encode with key < 32 bytes must return error")
	}
}

func TestEncodeUsesDefaultTTLWhenZero(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	c := Codec{Key: testKey32, Now: func() time.Time { return now }} // TTL = 0
	scope := validScope()
	raw, err := c.Encode(scope)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := c.Decode(raw, scope)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Expires != now.Add(24*time.Hour).Unix() {
		t.Fatalf("default TTL must be 24h, expires = %d, want %d", decoded.Expires, now.Add(24*time.Hour).Unix())
	}
}

func TestDecodeRejectsShortKey(t *testing.T) {
	c := Codec{Key: bytes.Repeat([]byte{1}, 16)}
	if _, err := c.Decode("a.b", validScope()); err == nil {
		t.Fatal("Decode with key < 32 bytes must return error")
	}
}

func TestDecodeRejectsLongCursor(t *testing.T) {
	c := validCodec()
	long := strings.Repeat("x", 4097) + "." + strings.Repeat("y", 10)
	if _, err := c.Decode(long, validScope()); err == nil {
		t.Fatal("Decode with cursor > 4096 bytes must return error")
	}
}

func TestDecodeRejectsCursorWithoutDot(t *testing.T) {
	c := validCodec()
	if _, err := c.Decode("nodothere", validScope()); err == nil {
		t.Fatal("Decode with no '.' separator must return error")
	}
}

func TestDecodeRejectsTamperedMAC(t *testing.T) {
	c := validCodec()
	scope := validScope()
	raw, err := c.Encode(scope)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.SplitN(raw, ".", 2)
	mac := []byte(parts[1])
	mac[0] ^= 0x01
	if _, err := c.Decode(parts[0]+"."+string(mac), scope); err == nil {
		t.Fatal("Decode with tampered MAC must return error")
	}
}

func TestDecodeRejectsBadBase64InMAC(t *testing.T) {
	c := validCodec()
	scope := validScope()
	raw, _ := c.Encode(scope)
	parts := strings.SplitN(raw, ".", 2)
	if _, err := c.Decode(parts[0]+".!!!invalid!!!", scope); err == nil {
		t.Fatal("Decode with invalid base64 MAC must return error")
	}
}

func TestDecodeRejectsBadBase64InData(t *testing.T) {
	c := validCodec()
	junk := "!!!not-base64!!!"
	// compute the correct HMAC so the MAC check passes, but base64 decode of data fails
	mac := auth.MAC(c.Key, "page-cursor", junk)
	encodedMAC := base64.RawURLEncoding.EncodeToString(mac)
	if _, err := c.Decode(junk+"."+encodedMAC, validScope()); err == nil {
		t.Fatal("Decode with bad base64 data (valid MAC) must return error")
	}
}
