package provenance

import (
	"bytes"
	"encoding/asn1"
	"testing"
)

func cmsEnvelopeFixture(t *testing.T, version int, eContentOID asn1.ObjectIdentifier, signers int) []byte {
	t.Helper()
	encode := func(value any) []byte {
		t.Helper()
		der, err := asn1.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return der
	}
	algorithm := encode(struct{ OID asn1.ObjectIdentifier }{rfc3161SHA256OID})
	digests := asn1.RawValue{Tag: asn1.TagSet, IsCompound: true, Bytes: algorithm}
	content := encode(bytes.Repeat([]byte{0x44}, 40))
	encapsulated := encode(struct {
		Type    asn1.ObjectIdentifier
		Content asn1.RawValue
	}{eContentOID, asn1.RawValue{Class: 2, Tag: 0, IsCompound: true, Bytes: content}})
	cert := encode(struct{ Serial int }{1})
	certificates := asn1.RawValue{Class: 2, Tag: 0, IsCompound: true, Bytes: cert}
	signer := encode(struct{ Version int }{1})
	signerSet := asn1.RawValue{Tag: asn1.TagSet, IsCompound: true, Bytes: bytes.Repeat(signer, signers)}
	signed := encode(struct {
		Version      int
		Digests      asn1.RawValue
		Encapsulated asn1.RawValue
		Certificates asn1.RawValue
		Signers      asn1.RawValue
	}{version, digests, asn1.RawValue{FullBytes: encapsulated}, certificates, signerSet})
	return encode(struct {
		Type    asn1.ObjectIdentifier
		Content asn1.RawValue
	}{cmsSignedDataOID, asn1.RawValue{Class: 2, Tag: 0, IsCompound: true, Bytes: signed}})
}

func TestRFC3161CMSExtractsOneUntrustedTSTInfo(t *testing.T) {
	token := cmsEnvelopeFixture(t, 3, rfc3161TSTInfoOID, 1)
	parts, err := parseRFC3161CMSParts(token)
	if err != nil || len(parts.TSTInfo) != 40 || len(parts.Certificates) != 1 || len(parts.SignerInfo) == 0 {
		t.Fatalf("CMS envelope: %v", err)
	}
	parts.TSTInfo[0] = '!'
	again, err := parseRFC3161CMSParts(token)
	if err != nil || again.TSTInfo[0] == '!' {
		t.Fatal("parser aliased untrusted token bytes")
	}
}

func TestRFC3161CMSRejectsUnsupportedEnvelope(t *testing.T) {
	good := cmsEnvelopeFixture(t, 3, rfc3161TSTInfoOID, 1)
	for name, token := range map[string][]byte{
		"old version": cmsEnvelopeFixture(t, 1, rfc3161TSTInfoOID, 1),
		"wrong type":  cmsEnvelopeFixture(t, 3, asn1.ObjectIdentifier{1, 2, 3}, 1),
		"no signer":   cmsEnvelopeFixture(t, 3, rfc3161TSTInfoOID, 0),
		"two signers": cmsEnvelopeFixture(t, 3, rfc3161TSTInfoOID, 2),
		"trailing":    append(append([]byte(nil), good...), 0),
		"truncated":   good[:len(good)-1],
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseRFC3161CMSParts(token); err == nil {
				t.Fatal("unsupported CMS token accepted")
			}
		})
	}
}
