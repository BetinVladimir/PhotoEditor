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

func TestRFC3161AccuracyStructure(t *testing.T) {
	cases := []struct {
		name string
		der  string
		ok   bool
	}{
		{"seconds-millis-micros", "3009020101800101810101", true},
		{"millis-only", "3003800101", true},
		{"empty", "3000", false},
		{"zero-seconds", "3003020100", false},
		{"negative-seconds", "30030201ff", false},
		{"nonminimal-seconds", "300402020001", false},
		{"millis-overflow", "3004800203e8", false},
		{"duplicate-millis", "3006800101800102", false},
		{"reversed", "3006810101800101", false},
		{"unknown-field", "3003820101", false},
		{"constructed-field", "3005a003020101", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			der, err := hex.DecodeString(tc.der)
			if err != nil {
				t.Fatal(err)
			}
			var value asn1.RawValue
			if rest, err := asn1.Unmarshal(der, &value); err != nil || len(rest) != 0 {
				t.Fatalf("invalid test DER: %v", err)
			}
			if got := rfc3161Accuracy(value); got != tc.ok {
				t.Fatalf("accuracy accepted=%v, want %v", got, tc.ok)
			}
		})
	}
}
