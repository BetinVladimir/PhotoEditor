package provenance

import (
	"bytes"
	"crypto/sha256"
	"encoding/asn1"
	"encoding/hex"
	"errors"
	"io"
	"math/big"
	"testing"
)

func TestRFC3161RequestBindsPayloadAndNonce(t *testing.T) {
	payload := []byte("signed-export-bytes")
	request, err := newRFC3161Request(payload, bytes.NewReader(bytes.Repeat([]byte{0x34}, 20)))
	if err != nil {
		t.Fatal(err)
	}
	var parsed rfc3161TimeStampRequest
	rest, err := asn1.Unmarshal(request.DER, &parsed)
	if err != nil || len(rest) != 0 || parsed.Version != 1 || !parsed.CertReq || !parsed.MessageImprint.Algorithm.OID.Equal(rfc3161SHA256OID) {
		t.Fatalf("invalid RFC3161 request: %v", err)
	}
	want := sha256.Sum256(payload)
	if !bytes.Equal(parsed.MessageImprint.HashedMessage, want[:]) || request.ImprintSHA != hex.EncodeToString(want[:]) || parsed.Nonce.Cmp(new(big.Int).SetBytes(request.Nonce)) != 0 || len(request.Nonce) != 20 || request.Nonce[0]&0x80 == 0 {
		t.Fatal("request lost imprint or nonce")
	}
	other, err := newRFC3161Request([]byte("different"), bytes.NewReader(bytes.Repeat([]byte{0x56}, 20)))
	if err != nil || other.ImprintSHA == request.ImprintSHA || bytes.Equal(other.Nonce, request.Nonce) {
		t.Fatal("distinct request reused imprint or nonce")
	}
}

func TestRFC3161RequestRejectsInvalidInput(t *testing.T) {
	if _, err := newRFC3161Request(nil, bytes.NewReader(make([]byte, 20))); !errors.Is(err, ErrInvalid) {
		t.Fatal("empty payload accepted", err)
	}
	if _, err := newRFC3161Request(make([]byte, 1<<20+1), bytes.NewReader(make([]byte, 20))); !errors.Is(err, ErrInvalid) {
		t.Fatal("oversized payload accepted", err)
	}
	if _, err := newRFC3161Request([]byte("x"), bytes.NewReader(make([]byte, 19))); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatal("short entropy accepted", err)
	}
}
