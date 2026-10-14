package provenance

import (
	"crypto/subtle"
	"encoding/asn1"
	"encoding/hex"
	"math/big"
	"time"
)

type rfc3161TSTInfoBinding struct {
	Policy    asn1.ObjectIdentifier
	Serial    *big.Int
	Generated time.Time
	Accuracy  []byte
	Ordering  bool
}

func rfc3161NextRaw(rest *[]byte) (asn1.RawValue, error) {
	var value asn1.RawValue
	remaining, err := asn1.Unmarshal(*rest, &value)
	if err != nil {
		return value, err
	}
	*rest = remaining
	return value, nil
}

func rfc3161AccuracyInteger(raw []byte, maximum int) bool {
	if len(raw) == 0 || len(raw) > 4 || raw[0]&0x80 != 0 ||
		(len(raw) > 1 && raw[0] == 0 && raw[1]&0x80 == 0) {
		return false
	}
	value := 0
	for _, octet := range raw {
		value = value<<8 | int(octet)
	}
	return value >= 1 && value <= maximum
}

func rfc3161Accuracy(raw asn1.RawValue) bool {
	if !rfc3161Sequence(raw) || len(raw.FullBytes) > 128 || len(raw.Bytes) == 0 {
		return false
	}
	last := -1
	for fields := raw.Bytes; len(fields) > 0; {
		value, err := rfc3161NextRaw(&fields)
		if err != nil || value.IsCompound {
			return false
		}
		rank, maximum := -1, 999
		switch {
		case value.Class == 0 && value.Tag == asn1.TagInteger:
			rank, maximum = 0, 86400
		case value.Class == 2 && value.Tag == 0:
			rank = 1
		case value.Class == 2 && value.Tag == 1:
			rank = 2
		}
		if rank <= last || !rfc3161AccuracyInteger(value.Bytes, maximum) {
			return false
		}
		last = rank
	}
	return true
}

// parseRFC3161TSTInfo binds an UNTRUSTED CMS eContent to the exact request.
// The caller must first verify CMS, ESSCertID and TSA trust independently.
// Unsupported optional TSA name/extensions fail closed until their signer
// identity and critical-extension semantics are implemented.
func parseRFC3161TSTInfo(raw []byte, request RFC3161Request) (rfc3161TSTInfoBinding, error) {
	var zero rfc3161TSTInfoBinding
	if len(raw) < 32 || len(raw) > 1<<20 || len(request.Nonce) != 20 || request.Nonce[0]&0x80 == 0 || len(request.ImprintSHA) != 64 {
		return zero, ErrInvalid
	}
	expectedHash, err := hex.DecodeString(request.ImprintSHA)
	if err != nil || hex.EncodeToString(expectedHash) != request.ImprintSHA {
		return zero, ErrInvalid
	}
	var outer asn1.RawValue
	trailing, err := asn1.Unmarshal(raw, &outer)
	if err != nil || len(trailing) != 0 || !rfc3161Sequence(outer) {
		return zero, ErrInvalid
	}
	fields := outer.Bytes
	var version int
	fields, err = asn1.Unmarshal(fields, &version)
	if err != nil || version != 1 {
		return zero, ErrInvalid
	}
	var policy asn1.ObjectIdentifier
	fields, err = asn1.Unmarshal(fields, &policy)
	if err != nil || len(policy) < 2 {
		return zero, ErrInvalid
	}
	imprint, err := rfc3161NextRaw(&fields)
	if err != nil || !rfc3161Sequence(imprint) {
		return zero, ErrInvalid
	}
	algorithm, err := rfc3161NextRaw(&imprint.Bytes)
	if err != nil || !rfc3161Sequence(algorithm) {
		return zero, ErrInvalid
	}
	var algorithmID asn1.ObjectIdentifier
	parameters, err := asn1.Unmarshal(algorithm.Bytes, &algorithmID)
	if err != nil || !algorithmID.Equal(rfc3161SHA256OID) {
		return zero, ErrInvalid
	}
	if len(parameters) > 0 {
		var null asn1.RawValue
		parameters, err = asn1.Unmarshal(parameters, &null)
		if err != nil || len(parameters) != 0 || null.Class != 0 || null.Tag != asn1.TagNull || len(null.Bytes) != 0 {
			return zero, ErrInvalid
		}
	}
	var hashed []byte
	imprint.Bytes, err = asn1.Unmarshal(imprint.Bytes, &hashed)
	if err != nil || len(imprint.Bytes) != 0 || len(hashed) != 32 || subtle.ConstantTimeCompare(hashed, expectedHash) != 1 {
		return zero, ErrInvalid
	}
	var serial *big.Int
	fields, err = asn1.Unmarshal(fields, &serial)
	if err != nil || serial == nil || serial.Sign() <= 0 || serial.BitLen() > 256 {
		return zero, ErrInvalid
	}
	generatedRaw, err := rfc3161NextRaw(&fields)
	if err != nil || generatedRaw.Class != 0 || generatedRaw.Tag != asn1.TagGeneralizedTime {
		return zero, ErrInvalid
	}
	var generated time.Time
	if rest, err := asn1.Unmarshal(generatedRaw.FullBytes, &generated); err != nil || len(rest) != 0 || generated.IsZero() {
		return zero, ErrInvalid
	}
	bound := rfc3161TSTInfoBinding{Policy: policy, Serial: serial, Generated: generated}
	last := 0
	foundNonce := false
	for len(fields) > 0 {
		value, err := rfc3161NextRaw(&fields)
		if err != nil {
			return zero, ErrInvalid
		}
		rank := 0
		switch {
		case value.Class == 0 && value.Tag == asn1.TagSequence:
			rank = 1
			if !rfc3161Accuracy(value) {
				return zero, ErrInvalid
			}
			bound.Accuracy = append([]byte(nil), value.FullBytes...)
		case value.Class == 0 && value.Tag == asn1.TagBoolean:
			rank = 2
			if rest, err := asn1.Unmarshal(value.FullBytes, &bound.Ordering); err != nil || len(rest) != 0 {
				return zero, ErrInvalid
			}
		case value.Class == 0 && value.Tag == asn1.TagInteger:
			rank = 3
			var nonce *big.Int
			if rest, err := asn1.Unmarshal(value.FullBytes, &nonce); err != nil || len(rest) != 0 || nonce == nil || nonce.Sign() <= 0 || subtle.ConstantTimeCompare(nonce.Bytes(), request.Nonce) != 1 {
				return zero, ErrInvalid
			}
			foundNonce = true
		default:
			return zero, ErrInvalid
		}
		if rank <= last {
			return zero, ErrInvalid
		}
		last = rank
	}
	if !foundNonce {
		return zero, ErrInvalid
	}
	return bound, nil
}
