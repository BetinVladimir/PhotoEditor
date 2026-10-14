package provenance

import "encoding/asn1"

var cmsSignedDataOID = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 2}

func rfc3161Sequence(value asn1.RawValue) bool {
	return value.Class == 0 && value.Tag == asn1.TagSequence && value.IsCompound && len(value.FullBytes) > 0
}

// parseRFC3161GrantedResponse only decodes the outer RFC3161/CMS envelope.
// Its output is UNTRUSTED. It must never satisfy SigningTrustCheck without
// independent CMS signature, TSA certificate and TSTInfo request binding.
func parseRFC3161GrantedResponse(raw []byte) ([]byte, error) {
	if len(raw) < 16 || len(raw) > 2<<20 {
		return nil, ErrInvalid
	}
	var outer, status, token, signedData asn1.RawValue
	rest, err := asn1.Unmarshal(raw, &outer)
	if err != nil || len(rest) != 0 || !rfc3161Sequence(outer) {
		return nil, ErrInvalid
	}
	fields, err := asn1.Unmarshal(outer.Bytes, &status)
	if err != nil || !rfc3161Sequence(status) {
		return nil, ErrInvalid
	}
	fields, err = asn1.Unmarshal(fields, &token)
	if err != nil || len(fields) != 0 || !rfc3161Sequence(token) {
		return nil, ErrInvalid
	}
	var code int
	statusRest, err := asn1.Unmarshal(status.Bytes, &code)
	if err != nil || code != 0 {
		// Modified grants and all failure/waiting statuses are not authority.
		return nil, ErrInvalid
	}
	if len(statusRest) > 0 {
		var text asn1.RawValue
		statusRest, err = asn1.Unmarshal(statusRest, &text)
		if err != nil || !rfc3161Sequence(text) || len(text.Bytes) > 4096 || len(statusRest) != 0 {
			return nil, ErrInvalid
		}
		for values := text.Bytes; len(values) > 0; {
			var value asn1.RawValue
			values, err = asn1.Unmarshal(values, &value)
			if err != nil || value.Class != 0 || value.Tag != asn1.TagUTF8String || len(value.Bytes) > 1024 {
				return nil, ErrInvalid
			}
		}
	}
	var contentType asn1.ObjectIdentifier
	contentRest, err := asn1.Unmarshal(token.Bytes, &contentType)
	if err != nil || !contentType.Equal(cmsSignedDataOID) {
		return nil, ErrInvalid
	}
	contentRest, err = asn1.Unmarshal(contentRest, &signedData)
	if err != nil || len(contentRest) != 0 || signedData.Class != 2 || signedData.Tag != 0 || !signedData.IsCompound {
		return nil, ErrInvalid
	}
	var inner asn1.RawValue
	innerRest, err := asn1.Unmarshal(signedData.Bytes, &inner)
	if err != nil || len(innerRest) != 0 || !rfc3161Sequence(inner) {
		return nil, ErrInvalid
	}
	return append([]byte(nil), token.FullBytes...), nil
}
