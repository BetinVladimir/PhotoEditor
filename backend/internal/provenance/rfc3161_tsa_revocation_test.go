package provenance

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"
)

type tsaCheckpointProbe struct{ calls int }

func (p *tsaCheckpointProbe) ObserveSigningCRL(context.Context, string, string, time.Time, string) error {
	p.calls++
	return nil
}

func TestDurableTSARevocationChecksSignedCRLAndCheckpoint(t *testing.T) {
	now := time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	rootTemplate := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "TSA root"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign, SubjectKeyId: []byte{1, 2, 3}}
	rootDER, err := x509.CreateCertificate(rand.Reader, rootTemplate, rootTemplate, key.Public(), key)
	if err != nil {
		t.Fatal(err)
	}
	root, err := x509.ParseCertificate(rootDER)
	if err != nil {
		t.Fatal(err)
	}
	leafTemplate := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "TSA"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTemplate, root, key.Public(), key)
	if err != nil {
		t.Fatal(err)
	}
	rootPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: rootDER})
	crl, err := x509.CreateRevocationList(rand.Reader,
		&x509.RevocationList{Number: big.NewInt(1), ThisUpdate: now.Add(-time.Minute), NextUpdate: now.Add(time.Minute)}, root, key)
	if err != nil {
		t.Fatal(err)
	}
	sourceCalls := 0
	source := func(_ context.Context, issuer []byte) ([]byte, error) {
		sourceCalls++
		if !bytes.Equal(issuer, rootDER) {
			t.Fatal("foreign CRL issuer")
		}
		return crl, nil
	}
	checkpoint := &tsaCheckpointProbe{}
	check, err := NewDurableRFC3161TSARevocationCheck(rootPEM, source,
		func() time.Time { return now }, time.Hour, checkpoint)
	if err != nil || check(context.Background(), [][]byte{leafDER, rootDER}) != nil || sourceCalls != 1 || checkpoint.calls != 1 {
		t.Fatalf("complete TSA CRL verification: %v calls=%d checkpoint=%d", err, sourceCalls, checkpoint.calls)
	}
	crl, err = x509.CreateRevocationList(rand.Reader,
		&x509.RevocationList{Number: big.NewInt(2), ThisUpdate: now.Add(-time.Minute), NextUpdate: now.Add(time.Minute),
			RevokedCertificateEntries: []x509.RevocationListEntry{{SerialNumber: big.NewInt(2), RevocationTime: now.Add(-time.Minute)}}}, root, key)
	if err != nil {
		t.Fatal(err)
	}
	if check(context.Background(), [][]byte{leafDER, rootDER}) == nil {
		t.Fatal("revoked TSA signer accepted")
	}
	if check(context.Background(), [][]byte{leafDER, leafDER}) == nil {
		t.Fatal("unanchored TSA path accepted")
	}
	if _, err := NewDurableRFC3161TSARevocationCheck(rootPEM, source, time.Now, time.Hour, nil); err == nil {
		t.Fatal("missing durable checkpoint accepted")
	}
}
