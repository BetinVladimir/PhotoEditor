package provenance

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/pem"
	"errors"
	"math/big"
	"sort"
	"testing"
	"time"
)

func tsaTrustFixture(t *testing.T, criticalEKU bool) ([]byte, []byte, RFC3161Request, time.Time) {
	t.Helper()
	encode := func(value any) []byte {
		t.Helper()
		raw, err := asn1.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	now := time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)
	rootKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	rootTemplate := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Pinned TSA root"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign}
	rootDER, err := x509.CreateCertificate(rand.Reader, rootTemplate, rootTemplate, rootKey.Public(), rootKey)
	if err != nil {
		t.Fatal(err)
	}
	root, err := x509.ParseCertificate(rootDER)
	if err != nil {
		t.Fatal(err)
	}
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	leafTemplate := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "TSA signer"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageTimeStamping}}
	if criticalEKU {
		leafTemplate.ExtraExtensions = []pkix.Extension{{Id: asn1.ObjectIdentifier{2, 5, 29, 37}, Critical: true,
			Value: encode([]asn1.ObjectIdentifier{{1, 3, 6, 1, 5, 5, 7, 3, 8}})}}
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTemplate, root, leafKey.Public(), rootKey)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(leafDER)
	if err != nil {
		t.Fatal(err)
	}
	request, err := newRFC3161Request([]byte("signed bytes"), bytes.NewReader(bytes.Repeat([]byte{0x45}, 20)))
	if err != nil {
		t.Fatal(err)
	}
	tstInfo := tstInfoFixture(t, mustSHABytes(t, request.ImprintSHA), new(big.Int).SetBytes(request.Nonce))
	attribute := func(oid asn1.ObjectIdentifier, value []byte) []byte {
		return encode(struct {
			Type   asn1.ObjectIdentifier
			Values asn1.RawValue
		}{oid,
			asn1.RawValue{Tag: asn1.TagSet, IsCompound: true, Bytes: value}})
	}
	contentHash, certHash := sha256.Sum256(tstInfo), sha256.Sum256(leafDER)
	ess := encode(struct{ Certs []struct{ Hash []byte } }{[]struct{ Hash []byte }{{certHash[:]}}})
	attrs := [][]byte{attribute(cmsContentTypeAttributeOID, encode(rfc3161TSTInfoOID)),
		attribute(cmsMessageDigestAttributeOID, encode(contentHash[:])),
		attribute(essSigningCertificateV2OID, ess)}
	sort.Slice(attrs, func(i, j int) bool { return bytes.Compare(attrs[i], attrs[j]) < 0 })
	joined := bytes.Join(attrs, nil)
	setDER := encode(asn1.RawValue{Tag: asn1.TagSet, IsCompound: true, Bytes: joined})
	signedHash := sha256.Sum256(setDER)
	signature, err := ecdsa.SignASN1(rand.Reader, leafKey, signedHash[:])
	if err != nil {
		t.Fatal(err)
	}
	sid := encode(struct {
		Issuer asn1.RawValue
		Serial *big.Int
	}{asn1.RawValue{FullBytes: leaf.RawIssuer}, leaf.SerialNumber})
	digestAlgorithm := encode(struct{ OID asn1.ObjectIdentifier }{rfc3161SHA256OID})
	signatureAlgorithm := encode(struct{ OID asn1.ObjectIdentifier }{ecdsaWithSHA256OID})
	signer := encode(struct {
		Version                       int
		SID, Digest, Attrs, Algorithm asn1.RawValue
		Signature                     []byte
	}{1, asn1.RawValue{FullBytes: sid}, asn1.RawValue{FullBytes: digestAlgorithm},
		asn1.RawValue{Class: 2, Tag: 0, IsCompound: true, Bytes: joined},
		asn1.RawValue{FullBytes: signatureAlgorithm}, signature})
	encapsulated := encode(struct {
		Type    asn1.ObjectIdentifier
		Content asn1.RawValue
	}{
		rfc3161TSTInfoOID, asn1.RawValue{Class: 2, Tag: 0, IsCompound: true, Bytes: encode(tstInfo)}})
	signed := encode(struct {
		Version                                      int
		Digests, Encapsulated, Certificates, Signers asn1.RawValue
	}{3, asn1.RawValue{Tag: asn1.TagSet, IsCompound: true, Bytes: digestAlgorithm},
		asn1.RawValue{FullBytes: encapsulated},
		asn1.RawValue{Class: 2, Tag: 0, IsCompound: true, Bytes: append(append([]byte(nil), leafDER...), rootDER...)},
		asn1.RawValue{Tag: asn1.TagSet, IsCompound: true, Bytes: signer}})
	token := encode(struct {
		Type    asn1.ObjectIdentifier
		Content asn1.RawValue
	}{
		cmsSignedDataOID, asn1.RawValue{Class: 2, Tag: 0, IsCompound: true, Bytes: signed}})
	response := encode(struct {
		Status struct{ Code int }
		Token  asn1.RawValue
	}{
		struct{ Code int }{0}, asn1.RawValue{FullBytes: token}})
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: rootDER}), response, request, now
}

func TestRFC3161TSATrustRequiresCriticalEKUAndLiveRevocation(t *testing.T) {
	roots, response, request, now := tsaTrustFixture(t, true)
	calls := 0
	check := func(ctx context.Context, chain [][]byte) error {
		calls++
		if len(chain) != 2 || len(chain[0]) == 0 || len(chain[1]) == 0 {
			t.Fatal("wrong TSA path")
		}
		return nil
	}
	trust, err := NewRFC3161TSATrust(roots, asn1.ObjectIdentifier{1, 2, 3, 4}, func() time.Time { return now }, time.Minute, check)
	if err != nil || trust.Verify(context.Background(), response, request) != nil || calls != 1 {
		t.Fatalf("valid TSA trust: %v calls=%d", err, calls)
	}
	wrongRequest := request
	wrongRequest.Nonce = bytes.Repeat([]byte{0x91}, 20)
	if trust.Verify(context.Background(), response, wrongRequest) == nil || calls != 1 {
		t.Fatal("foreign request reached revocation")
	}
	roots, response, request, now = tsaTrustFixture(t, false)
	trust, err = NewRFC3161TSATrust(roots, asn1.ObjectIdentifier{1, 2, 3, 4}, func() time.Time { return now }, time.Minute, check)
	if err != nil || trust.Verify(context.Background(), response, request) == nil || calls != 1 {
		t.Fatal("noncritical TSA EKU accepted")
	}
	if _, err := NewRFC3161TSATrust(roots, asn1.ObjectIdentifier{1, 2, 3, 4}, time.Now, time.Minute, nil); !errors.Is(err, ErrInvalid) {
		t.Fatal("missing revocation authority accepted")
	}
}

func TestRFC3161TSATrustRejectsStaleTimeAndRevocationRefusal(t *testing.T) {
	roots, response, request, now := tsaTrustFixture(t, true)
	refused := errors.New("TSA certificate revoked")
	trust, err := NewRFC3161TSATrust(roots, asn1.ObjectIdentifier{1, 2, 3, 4},
		func() time.Time { return now }, time.Minute,
		func(context.Context, [][]byte) error { return refused })
	if err != nil || !errors.Is(trust.Verify(context.Background(), response, request), refused) {
		t.Fatal("revocation refusal lost", err)
	}
	trust, err = NewRFC3161TSATrust(roots, asn1.ObjectIdentifier{1, 2, 3, 4},
		func() time.Time { return now.Add(2 * time.Minute) }, time.Minute,
		func(context.Context, [][]byte) error { t.Fatal("stale token reached revocation"); return nil })
	if err != nil || trust.Verify(context.Background(), response, request) == nil {
		t.Fatal("stale TSA timestamp accepted", err)
	}
}
