package provenance

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/asn1"
	"encoding/hex"
	"io"
	"math/big"
)

var rfc3161SHA256OID = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 1}

type RFC3161Request struct {
	DER        []byte
	Nonce      []byte
	ImprintSHA string
}

type rfc3161MessageImprint struct {
	Algorithm     struct{ OID asn1.ObjectIdentifier }
	HashedMessage []byte
}

type rfc3161TimeStampRequest struct {
	Version        int
	MessageImprint rfc3161MessageImprint
	Nonce          *big.Int
	CertReq        bool
}

// NewRFC3161Request hashes the exact caller-owned bytes and generates a fresh
// positive 160-bit nonce. A response is not trusted until its CMS signature,
// TSA certificate, nonce and message imprint have all been verified.
func NewRFC3161Request(payload []byte) (RFC3161Request, error) {
	return newRFC3161Request(payload, rand.Reader)
}

func newRFC3161Request(payload []byte, randomness io.Reader) (RFC3161Request, error) {
	if len(payload) < 1 || len(payload) > 1<<20 || randomness == nil {
		return RFC3161Request{}, ErrInvalid
	}
	nonce := make([]byte, 20)
	if _, err := io.ReadFull(randomness, nonce); err != nil {
		return RFC3161Request{}, err
	}
	// Keep the nonce at exactly 160 significant bits. ASN.1 adds a leading
	// zero octet to encode the resulting positive INTEGER.
	nonce[0] |= 0x80
	imprint := sha256.Sum256(payload)
	request := rfc3161TimeStampRequest{Version: 1, Nonce: new(big.Int).SetBytes(nonce), CertReq: true}
	request.MessageImprint.Algorithm.OID = rfc3161SHA256OID
	request.MessageImprint.HashedMessage = imprint[:]
	der, err := asn1.Marshal(request)
	if err != nil || len(der) > 4096 {
		return RFC3161Request{}, ErrInvalid
	}
	return RFC3161Request{DER: der, Nonce: nonce, ImprintSHA: hex.EncodeToString(imprint[:])}, nil
}
