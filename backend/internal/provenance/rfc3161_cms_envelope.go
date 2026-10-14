package provenance

import "encoding/asn1"

var rfc3161TSTInfoOID = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 16, 1, 4}

// rfc3161CMSParts are parsed bytes only. They carry no verified signature or
// trust claim and must not leave the qualified verifier as an approval.
type rfc3161CMSParts struct {
	TSTInfo      []byte
	Certificates [][]byte
	SignerInfo   []byte
}

func rfc3161Set(value asn1.RawValue) bool {
	return value.Class == 0 && value.Tag == asn1.TagSet && value.IsCompound
}

func parseRFC3161CMSParts(token []byte) (rfc3161CMSParts, error) {
	var zero rfc3161CMSParts
	if len(token) < 32 || len(token) > 2<<20 {
		return zero, ErrInvalid
	}
	var outer asn1.RawValue
	rest, err := asn1.Unmarshal(token, &outer)
	if err != nil || len(rest) != 0 || !rfc3161Sequence(outer) {
		return zero, ErrInvalid
	}
	var contentType asn1.ObjectIdentifier
	fields, err := asn1.Unmarshal(outer.Bytes, &contentType)
	if err != nil || !contentType.Equal(cmsSignedDataOID) {
		return zero, ErrInvalid
	}
	wrapper, err := rfc3161NextRaw(&fields)
	if err != nil || len(fields) != 0 || wrapper.Class != 2 || wrapper.Tag != 0 || !wrapper.IsCompound {
		return zero, ErrInvalid
	}
	signed, err := rfc3161NextRaw(&wrapper.Bytes)
	if err != nil || len(wrapper.Bytes) != 0 || !rfc3161Sequence(signed) {
		return zero, ErrInvalid
	}
	fields = signed.Bytes
	var version int
	fields, err = asn1.Unmarshal(fields, &version)
	if err != nil || version != 3 {
		return zero, ErrInvalid
	}
	digests, err := rfc3161NextRaw(&fields)
	if err != nil || !rfc3161Set(digests) {
		return zero, ErrInvalid
	}
	digestAlgorithm, err := rfc3161NextRaw(&digests.Bytes)
	if err != nil || len(digests.Bytes) != 0 || !rfc3161Sequence(digestAlgorithm) {
		return zero, ErrInvalid
	}
	var digestOID asn1.ObjectIdentifier
	parameters, err := asn1.Unmarshal(digestAlgorithm.Bytes, &digestOID)
	if err != nil || !digestOID.Equal(rfc3161SHA256OID) {
		return zero, ErrInvalid
	}
	if len(parameters) > 0 {
		var null asn1.RawValue
		parameters, err = asn1.Unmarshal(parameters, &null)
		if err != nil || len(parameters) != 0 || null.Class != 0 || null.Tag != asn1.TagNull || len(null.Bytes) != 0 {
			return zero, ErrInvalid
		}
	}
	encapsulated, err := rfc3161NextRaw(&fields)
	if err != nil || !rfc3161Sequence(encapsulated) {
		return zero, ErrInvalid
	}
	var innerType asn1.ObjectIdentifier
	innerFields, err := asn1.Unmarshal(encapsulated.Bytes, &innerType)
	if err != nil || !innerType.Equal(rfc3161TSTInfoOID) {
		return zero, ErrInvalid
	}
	wrappedContent, err := rfc3161NextRaw(&innerFields)
	if err != nil || len(innerFields) != 0 || wrappedContent.Class != 2 || wrappedContent.Tag != 0 || !wrappedContent.IsCompound {
		return zero, ErrInvalid
	}
	var tstInfo []byte
	innerFields, err = asn1.Unmarshal(wrappedContent.Bytes, &tstInfo)
	if err != nil || len(innerFields) != 0 || len(tstInfo) < 32 || len(tstInfo) > 1<<20 {
		return zero, ErrInvalid
	}
	certificates, err := rfc3161NextRaw(&fields)
	if err != nil || certificates.Class != 2 || certificates.Tag != 0 || !certificates.IsCompound {
		return zero, ErrInvalid
	}
	result := rfc3161CMSParts{TSTInfo: append([]byte(nil), tstInfo...)}
	for remaining := certificates.Bytes; len(remaining) > 0; {
		if len(result.Certificates) >= 16 {
			return zero, ErrInvalid
		}
		certificate, err := rfc3161NextRaw(&remaining)
		if err != nil || !rfc3161Sequence(certificate) || len(certificate.FullBytes) > 1<<20 {
			return zero, ErrInvalid
		}
		result.Certificates = append(result.Certificates, append([]byte(nil), certificate.FullBytes...))
	}
	if len(result.Certificates) == 0 {
		return zero, ErrInvalid
	}
	signers, err := rfc3161NextRaw(&fields)
	if err != nil || len(fields) != 0 || !rfc3161Set(signers) {
		return zero, ErrInvalid
	}
	signer, err := rfc3161NextRaw(&signers.Bytes)
	if err != nil || len(signers.Bytes) != 0 || !rfc3161Sequence(signer) || len(signer.FullBytes) > 1<<20 {
		return zero, ErrInvalid
	}
	result.SignerInfo = append([]byte(nil), signer.FullBytes...)
	return result, nil
}
