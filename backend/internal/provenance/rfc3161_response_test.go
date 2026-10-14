package provenance

import (
	"bytes"
	"encoding/asn1"
	"testing"
)

func rfc3161EnvelopeFixture(t *testing.T, status int, oid asn1.ObjectIdentifier) []byte {
	t.Helper()
	inner, err := asn1.Marshal(struct{ Version int }{1})
	if err != nil {
		t.Fatal(err)
	}
	token, err := asn1.Marshal(struct {
		Type    asn1.ObjectIdentifier
		Content asn1.RawValue
	}{oid, asn1.RawValue{Class: 2, Tag: 0, IsCompound: true, Bytes: inner}})
	if err != nil {
		t.Fatal(err)
	}
	outer, err := asn1.Marshal(struct {
		Status struct{ Code int }
		Token  asn1.RawValue
	}{struct{ Code int }{status}, asn1.RawValue{FullBytes: token}})
	if err != nil {
		t.Fatal(err)
	}
	return outer
}

func TestRFC3161ResponseEnvelopeRemainsUntrusted(t *testing.T) {
	raw := rfc3161EnvelopeFixture(t, 0, cmsSignedDataOID)
	token, err := parseRFC3161GrantedResponse(raw)
	if err != nil || len(token) == 0 {
		t.Fatalf("valid outer envelope: %v", err)
	}
	token[0] = '!'
	again, err := parseRFC3161GrantedResponse(raw)
	if err != nil || again[0] == '!' {
		t.Fatal("response parser aliased caller bytes")
	}
}

func TestRFC3161ResponseEnvelopeRejectsMalformedAndFailure(t *testing.T) {
	good := rfc3161EnvelopeFixture(t, 0, cmsSignedDataOID)
	for name, raw := range map[string][]byte{
		"empty":          nil,
		"truncated":      good[:len(good)-1],
		"trailing":       append(append([]byte(nil), good...), 0),
		"waiting":        rfc3161EnvelopeFixture(t, 3, cmsSignedDataOID),
		"modified":       rfc3161EnvelopeFixture(t, 1, cmsSignedDataOID),
		"wrong CMS type": rfc3161EnvelopeFixture(t, 0, asn1.ObjectIdentifier{1, 2, 3}),
		"oversized":      bytes.Repeat([]byte{0}, 2<<20+1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseRFC3161GrantedResponse(raw); err == nil {
				t.Fatal("untrusted response accepted")
			}
		})
	}
}
