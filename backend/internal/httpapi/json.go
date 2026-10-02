package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"photoeditor/backend/internal/domain"
	"unicode/utf8"
)

// Decode rejects duplicate keys at every depth, unknown fields, trailing values,
// invalid UTF-8 and unpaired Unicode surrogates before typed decoding.
func decodeJSON(r *http.Request, dst any) error {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		return domain.NewError("UNSUPPORTED_MEDIA_TYPE", 415)
	}
	b, err := io.ReadAll(r.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return domain.NewError("BODY_TOO_LARGE", 413)
		}
		return domain.NewError("INVALID_JSON", 400)
	}
	if !utf8.Valid(b) || !validEscapes(b) {
		return domain.NewError("INVALID_JSON", 400)
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if err = uniqueValue(d); err != nil {
		return domain.NewError("INVALID_JSON", 400)
	}
	if _, err = d.Token(); err != io.EOF {
		return domain.NewError("INVALID_JSON", 400)
	}
	d = json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err = d.Decode(dst); err != nil {
		return domain.NewError("VALIDATION_FAILED", 422)
	}
	return nil
}
func uniqueValue(d *json.Decoder) error {
	v, err := d.Token()
	if err != nil {
		return err
	}
	delim, ok := v.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			v, err = d.Token()
			if err != nil {
				return err
			}
			key, ok := v.(string)
			if !ok || seen[key] {
				return errors.New("duplicate key")
			}
			seen[key] = true
			if err = uniqueValue(d); err != nil {
				return err
			}
		}
		v, err = d.Token()
		if err != nil || v != json.Delim('}') {
			return errors.New("invalid object")
		}
	case '[':
		for d.More() {
			if err = uniqueValue(d); err != nil {
				return err
			}
		}
		v, err = d.Token()
		if err != nil || v != json.Delim(']') {
			return errors.New("invalid array")
		}
	default:
		return errors.New("unexpected delimiter")
	}
	return nil
}
func hex4(b []byte) (uint16, bool) {
	if len(b) < 4 {
		return 0, false
	}
	var n uint16
	for _, c := range b[:4] {
		n <<= 4
		switch {
		case c >= '0' && c <= '9':
			n += uint16(c - '0')
		case c >= 'a' && c <= 'f':
			n += uint16(c - 'a' + 10)
		case c >= 'A' && c <= 'F':
			n += uint16(c - 'A' + 10)
		default:
			return 0, false
		}
	}
	return n, true
}
func validEscapes(b []byte) bool {
	for i := 0; i < len(b); i++ {
		if b[i] != '\\' {
			continue
		}
		i++
		if i >= len(b) {
			return false
		}
		if b[i] != 'u' {
			continue
		}
		n, ok := hex4(b[i+1:])
		if !ok {
			return false
		}
		i += 4
		if n >= 0xdc00 && n <= 0xdfff {
			return false
		}
		if n >= 0xd800 && n <= 0xdbff {
			if i+6 >= len(b) || b[i+1] != '\\' || b[i+2] != 'u' {
				return false
			}
			low, ok := hex4(b[i+3:])
			if !ok || low < 0xdc00 || low > 0xdfff {
				return false
			}
			i += 6
		}
	}
	return true
}
