package provenance

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"encoding/asn1"
	"math/big"
)

var (
	cmsContentTypeAttributeOID   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 3}
	cmsMessageDigestAttributeOID = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 4}
	essSigningCertificateV2OID   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 16, 2, 47}
	sha256WithRSAOID             = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 11}
	ecdsaWithSHA256OID           = asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 3, 2}
)

func rfc3161Algorithm(raw asn1.RawValue, expected asn1.ObjectIdentifier, allowNull bool) bool {
	if !rfc3161Sequence(raw) {
		return false
	}
	var oid asn1.ObjectIdentifier
	rest, err := asn1.Unmarshal(raw.Bytes, &oid)
	if err != nil || !oid.Equal(expected) {
		return false
	}
	if len(rest) == 0 {
		return true
	}
	if !allowNull {
		return false
	}
	var null asn1.RawValue
	rest, err = asn1.Unmarshal(rest, &null)
	return err == nil && len(rest) == 0 && null.Class == 0 && null.Tag == asn1.TagNull && len(null.Bytes) == 0
}

func rfc3161SignerCertificate(parts rfc3161CMSParts, sid asn1.RawValue) (*x509.Certificate, error) {
	if !rfc3161Sequence(sid) {
		return nil, ErrInvalid
	}
	issuer, err := rfc3161NextRaw(&sid.Bytes)
	if err != nil || !rfc3161Sequence(issuer) {
		return nil, ErrInvalid
	}
	var serial *big.Int
	remaining, err := asn1.Unmarshal(sid.Bytes, &serial)
	if err != nil || len(remaining) != 0 || serial == nil || serial.Sign() <= 0 {
		return nil, ErrInvalid
	}
	var selected *x509.Certificate
	for _, raw := range parts.Certificates {
		cert, err := x509.ParseCertificate(raw)
		if err != nil {
			return nil, ErrInvalid
		}
		if bytes.Equal(cert.RawIssuer, issuer.FullBytes) && cert.SerialNumber.Cmp(serial) == 0 {
			if selected != nil {
				return nil, ErrInvalid
			}
			selected = cert
		}
	}
	if selected == nil {
		return nil, ErrInvalid
	}
	return selected, nil
}

func rfc3161ESSCertIDv2(value asn1.RawValue, cert *x509.Certificate) bool {
	if cert == nil || !rfc3161Sequence(value) {
		return false
	}
	certs, err := rfc3161NextRaw(&value.Bytes)
	if err != nil || len(value.Bytes) != 0 || !rfc3161Sequence(certs) {
		return false
	}
	first, err := rfc3161NextRaw(&certs.Bytes)
	if err != nil || len(certs.Bytes) != 0 || !rfc3161Sequence(first) {
		return false
	}
	field, err := rfc3161NextRaw(&first.Bytes)
	if err != nil {
		return false
	}
	if rfc3161Sequence(field) {
		if !rfc3161Algorithm(field, rfc3161SHA256OID, true) {
			return false
		}
		field, err = rfc3161NextRaw(&first.Bytes)
		if err != nil {
			return false
		}
	}
	if field.Class != 0 || field.Tag != asn1.TagOctetString || len(field.Bytes) != 32 {
		return false
	}
	sum := sha256.Sum256(cert.Raw)
	if subtle.ConstantTimeCompare(field.Bytes, sum[:]) != 1 {
		return false
	}
	if len(first.Bytes) == 0 {
		return true
	}
	issuerSerial, err := rfc3161NextRaw(&first.Bytes)
	if err != nil || len(first.Bytes) != 0 || !rfc3161Sequence(issuerSerial) {
		return false
	}
	names, err := rfc3161NextRaw(&issuerSerial.Bytes)
	if err != nil || !rfc3161Sequence(names) {
		return false
	}
	name, err := rfc3161NextRaw(&names.Bytes)
	if err != nil || len(names.Bytes) != 0 || name.Class != 2 || name.Tag != 4 || !name.IsCompound {
		return false
	}
	var directoryName asn1.RawValue
	nameRest, err := asn1.Unmarshal(name.Bytes, &directoryName)
	if err != nil || len(nameRest) != 0 || !rfc3161Sequence(directoryName) || !bytes.Equal(directoryName.FullBytes, cert.RawIssuer) {
		return false
	}
	var serial *big.Int
	serialRest, err := asn1.Unmarshal(issuerSerial.Bytes, &serial)
	return err == nil && len(serialRest) == 0 && serial != nil && serial.Cmp(cert.SerialNumber) == 0
}

func rfc3161SignedAttributes(attrs asn1.RawValue, content []byte, cert *x509.Certificate) ([]byte, error) {
	if attrs.Class != 2 || attrs.Tag != 0 || !attrs.IsCompound || len(attrs.FullBytes) < 2 || attrs.FullBytes[0] != 0xa0 {
		return nil, ErrInvalid
	}
	signedDER := append([]byte(nil), attrs.FullBytes...)
	signedDER[0] = 0x31 // CMS signs DER SET OF, not the IMPLICIT [0] wrapper.
	seen := [3]bool{}
	var previous []byte
	for fields := attrs.Bytes; len(fields) > 0; {
		attribute, err := rfc3161NextRaw(&fields)
		if err != nil || !rfc3161Sequence(attribute) || previous != nil && bytes.Compare(previous, attribute.FullBytes) >= 0 {
			return nil, ErrInvalid
		}
		previous = attribute.FullBytes
		var oid asn1.ObjectIdentifier
		values, err := asn1.Unmarshal(attribute.Bytes, &oid)
		if err != nil {
			return nil, ErrInvalid
		}
		set, err := rfc3161NextRaw(&values)
		if err != nil || len(values) != 0 || !rfc3161Set(set) {
			return nil, ErrInvalid
		}
		value, err := rfc3161NextRaw(&set.Bytes)
		if err != nil || len(set.Bytes) != 0 {
			return nil, ErrInvalid
		}
		switch {
		case oid.Equal(cmsContentTypeAttributeOID):
			if seen[0] {
				return nil, ErrInvalid
			}
			seen[0] = true
			var actual asn1.ObjectIdentifier
			rest, err := asn1.Unmarshal(value.FullBytes, &actual)
			if err != nil || len(rest) != 0 || !actual.Equal(rfc3161TSTInfoOID) {
				return nil, ErrInvalid
			}
		case oid.Equal(cmsMessageDigestAttributeOID):
			if seen[1] || value.Class != 0 || value.Tag != asn1.TagOctetString {
				return nil, ErrInvalid
			}
			seen[1] = true
			sum := sha256.Sum256(content)
			if subtle.ConstantTimeCompare(value.Bytes, sum[:]) != 1 {
				return nil, ErrInvalid
			}
		case oid.Equal(essSigningCertificateV2OID):
			if seen[2] || !rfc3161ESSCertIDv2(value, cert) {
				return nil, ErrInvalid
			}
			seen[2] = true
		default:
			// Unknown signed attributes are rejected by this qualified profile.
			return nil, ErrInvalid
		}
	}
	if !seen[0] || !seen[1] || !seen[2] {
		return nil, ErrInvalid
	}
	return signedDER, nil
}

// verifyRFC3161CMSSignature establishes content integrity and signer-key
// possession only. The returned certificate is NOT trusted until a separate
// TSA chain/EKU/revocation/clock check succeeds.
func verifyRFC3161CMSSignature(parts rfc3161CMSParts) (*x509.Certificate, error) {
	if len(parts.TSTInfo) < 32 || len(parts.TSTInfo) > 1<<20 || len(parts.SignerInfo) < 16 || len(parts.SignerInfo) > 1<<20 {
		return nil, ErrInvalid
	}
	var signer asn1.RawValue
	rest, err := asn1.Unmarshal(parts.SignerInfo, &signer)
	if err != nil || len(rest) != 0 || !rfc3161Sequence(signer) {
		return nil, ErrInvalid
	}
	fields := signer.Bytes
	var version int
	fields, err = asn1.Unmarshal(fields, &version)
	if err != nil || version != 1 {
		return nil, ErrInvalid
	}
	sid, err := rfc3161NextRaw(&fields)
	if err != nil {
		return nil, ErrInvalid
	}
	cert, err := rfc3161SignerCertificate(parts, sid)
	if err != nil {
		return nil, err
	}
	digestAlgorithm, err := rfc3161NextRaw(&fields)
	if err != nil || !rfc3161Algorithm(digestAlgorithm, rfc3161SHA256OID, true) {
		return nil, ErrInvalid
	}
	attrs, err := rfc3161NextRaw(&fields)
	if err != nil {
		return nil, ErrInvalid
	}
	signedDER, err := rfc3161SignedAttributes(attrs, parts.TSTInfo, cert)
	if err != nil {
		return nil, err
	}
	signatureAlgorithm, err := rfc3161NextRaw(&fields)
	if err != nil || !rfc3161Sequence(signatureAlgorithm) {
		return nil, ErrInvalid
	}
	var oid asn1.ObjectIdentifier
	_, err = asn1.Unmarshal(signatureAlgorithm.Bytes, &oid)
	if err != nil {
		return nil, ErrInvalid
	}
	var algorithm x509.SignatureAlgorithm
	switch {
	case oid.Equal(ecdsaWithSHA256OID) && rfc3161Algorithm(signatureAlgorithm, ecdsaWithSHA256OID, false):
		algorithm = x509.ECDSAWithSHA256
	case oid.Equal(sha256WithRSAOID) && rfc3161Algorithm(signatureAlgorithm, sha256WithRSAOID, true):
		algorithm = x509.SHA256WithRSA
	default:
		return nil, ErrInvalid
	}
	var signature []byte
	fields, err = asn1.Unmarshal(fields, &signature)
	if err != nil || len(fields) != 0 || len(signature) < 16 || len(signature) > 16384 {
		return nil, ErrInvalid
	}
	if err := cert.CheckSignature(algorithm, signedDER, signature); err != nil {
		return nil, ErrInvalid
	}
	return cert, nil
}
