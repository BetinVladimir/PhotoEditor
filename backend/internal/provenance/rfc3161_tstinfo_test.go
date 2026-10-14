package provenance

import (
	"bytes"
	"encoding/asn1"
	"encoding/hex"
	"math/big"
	"testing"
	"time"
)

func tstInfoFixture(t *testing.T, hash []byte, nonce *big.Int) []byte {
	t.Helper()
	var imprint rfc3161MessageImprint
	imprint.Algorithm.OID = rfc3161SHA256OID
	imprint.HashedMessage = hash
	type body struct {
		Version   int
		Policy    asn1.ObjectIdentifier
		Imprint   rfc3161MessageImprint
		Serial    *big.Int
		Generated time.Time `asn1:"generalized"`
		Nonce     *big.Int
	}
	base := body{1, asn1.ObjectIdentifier{1, 2, 3, 4}, imprint, big.NewInt(42),
		time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC), nonce}
	var raw []byte
	var err error
	if nonce == nil {
		raw, err = asn1.Marshal(struct {
			Version   int
			Policy    asn1.ObjectIdentifier
			Imprint   rfc3161MessageImprint
			Serial    *big.Int
			Generated time.Time `asn1:"generalized"`
		}{base.Version, base.Policy, base.Imprint, base.Serial, base.Generated})
	} else {
		raw, err = asn1.Marshal(base)
	}
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestRFC3161TSTInfoBindsRequest(t *testing.T) {
	request, err := newRFC3161Request([]byte("signed bytes"), bytes.NewReader(bytes.Repeat([]byte{0x45}, 20)))
	if err != nil {
		t.Fatal(err)
	}
	hash := mustSHABytes(t, request.ImprintSHA)
	nonce := new(big.Int).SetBytes(request.Nonce)
	parsed, err := parseRFC3161TSTInfo(tstInfoFixture(t, hash, nonce), request)
	if err != nil || parsed.Serial.Cmp(big.NewInt(42)) != 0 || !parsed.Policy.Equal(asn1.ObjectIdentifier{1, 2, 3, 4}) {
		t.Fatalf("bound TSTInfo: %v", err)
	}
	wrongHash := append([]byte(nil), hash...)
	wrongHash[0] ^= 1
	if _, err := parseRFC3161TSTInfo(tstInfoFixture(t, wrongHash, nonce), request); err == nil {
		t.Fatal("foreign message imprint accepted")
	}
	if _, err := parseRFC3161TSTInfo(tstInfoFixture(t, hash, big.NewInt(99)), request); err == nil {
		t.Fatal("foreign nonce accepted")
	}
	if _, err := parseRFC3161TSTInfo(tstInfoFixture(t, hash, nil), request); err == nil {
		t.Fatal("missing nonce accepted")
	}
}

func mustSHABytes(t *testing.T, value string) []byte {
	t.Helper()
	raw, err := hex.DecodeString(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
