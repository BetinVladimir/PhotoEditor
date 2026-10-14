package provenance

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/asn1"
	"math/big"
	"sort"
	"testing"
	"time"
)

func signedCMSPartsFixture(t *testing.T, includeESS bool) rfc3161CMSParts {
	t.Helper()
	encode := func(value any) []byte {
		t.Helper()
		der, err := asn1.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return der
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	template := &x509.Certificate{SerialNumber: big.NewInt(7), NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature}
	certDER, err := x509.CreateCertificate(rand.Reader, template, template, key.Public(), key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		t.Fatal(err)
	}
	content := bytes.Repeat([]byte{0x42}, 64)
	attribute := func(oid asn1.ObjectIdentifier, value []byte) []byte {
		return encode(struct {
			Type   asn1.ObjectIdentifier
			Values asn1.RawValue
		}{oid, asn1.RawValue{Tag: asn1.TagSet, IsCompound: true, Bytes: value}})
	}
	sum := sha256.Sum256(content)
	attrs := [][]byte{
		attribute(cmsContentTypeAttributeOID, encode(rfc3161TSTInfoOID)),
		attribute(cmsMessageDigestAttributeOID, encode(sum[:])),
	}
	if includeESS {
		certSum := sha256.Sum256(certDER)
		ess := encode(struct{ Certs []struct{ Hash []byte } }{
			Certs: []struct{ Hash []byte }{{certSum[:]}},
		})
		attrs = append(attrs, attribute(essSigningCertificateV2OID, ess))
	}
	sort.Slice(attrs, func(i, j int) bool { return bytes.Compare(attrs[i], attrs[j]) < 0 })
	joined := bytes.Join(attrs, nil)
	signedDER := encode(asn1.RawValue{Tag: asn1.TagSet, IsCompound: true, Bytes: joined})
	hash := sha256.Sum256(signedDER)
	signature, err := ecdsa.SignASN1(rand.Reader, key, hash[:])
	if err != nil {
		t.Fatal(err)
	}
	sid := encode(struct {
		Issuer asn1.RawValue
		Serial *big.Int
	}{asn1.RawValue{FullBytes: cert.RawIssuer}, cert.SerialNumber})
	digestAlgorithm := encode(struct{ OID asn1.ObjectIdentifier }{rfc3161SHA256OID})
	signatureAlgorithm := encode(struct{ OID asn1.ObjectIdentifier }{ecdsaWithSHA256OID})
	signer := encode(struct {
		Version   int
		SID       asn1.RawValue
		Digest    asn1.RawValue
		Attrs     asn1.RawValue
		Algorithm asn1.RawValue
		Signature []byte
	}{1, asn1.RawValue{FullBytes: sid}, asn1.RawValue{FullBytes: digestAlgorithm},
		asn1.RawValue{Class: 2, Tag: 0, IsCompound: true, Bytes: joined},
		asn1.RawValue{FullBytes: signatureAlgorithm}, signature})
	return rfc3161CMSParts{TSTInfo: content, Certificates: [][]byte{certDER}, SignerInfo: signer}
}

func TestRFC3161CMSSignatureAndESSBinding(t *testing.T) {
	parts := signedCMSPartsFixture(t, true)
	cert, err := verifyRFC3161CMSSignature(parts)
	if err != nil || cert == nil || !bytes.Equal(cert.Raw, parts.Certificates[0]) {
		t.Fatalf("valid signature: %v", err)
	}
	parts.TSTInfo[0] ^= 1
	if _, err := verifyRFC3161CMSSignature(parts); err == nil {
		t.Fatal("modified content accepted")
	}
	parts = signedCMSPartsFixture(t, false)
	if _, err := verifyRFC3161CMSSignature(parts); err == nil {
		t.Fatal("missing ESSCertIDv2 accepted")
	}
	parts = signedCMSPartsFixture(t, true)
	parts.Certificates[0][len(parts.Certificates[0])-1] ^= 1
	if _, err := verifyRFC3161CMSSignature(parts); err == nil {
		t.Fatal("modified certificate accepted")
	}
}
