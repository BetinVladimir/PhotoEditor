package canonical

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	jcs "github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
	"io"
	"math/big"
	"strings"
	"unicode/utf8"
)

func Bytes(raw []byte) ([]byte, error) {
	if !utf8.Valid(raw) {
		return nil, errors.New("invalid UTF-8")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var value any
	if err := d.Decode(&value); err != nil {
		return nil, err
	}
	var trailing any
	if d.Decode(&trailing) != io.EOF {
		return nil, errors.New("trailing JSON")
	}
	if err := safeIntegers(value); err != nil {
		return nil, err
	}
	// The upstream parser accepts only container roots. RFC8785 also permits
	// scalar JSON values; canonicalize those through an unambiguous singleton
	// array, preserving the same number/string implementation and validation.
	switch value.(type) {
	case map[string]any, []any:
		return jcs.Transform(raw)
	}
	wrapped := append([]byte{'['}, raw...)
	wrapped = append(wrapped, ']')
	normalized, err := jcs.Transform(wrapped)
	if err != nil {
		return nil, err
	}
	return append([]byte(nil), normalized[1:len(normalized)-1]...), nil
}
func safeIntegers(v any) error {
	switch x := v.(type) {
	case json.Number:
		if !strings.ContainsAny(string(x), ".eE") {
			n, ok := new(big.Int).SetString(string(x), 10)
			if !ok || n.Abs(n).Cmp(big.NewInt(9007199254740991)) > 0 {
				return errors.New("integer exceeds interoperable range")
			}
		}
	case map[string]any:
		for _, a := range x {
			if err := safeIntegers(a); err != nil {
				return err
			}
		}
	case []any:
		for _, a := range x {
			if err := safeIntegers(a); err != nil {
				return err
			}
		}
	}
	return nil
}
func Marshal(v any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return Bytes(b)
}
func Hash(v any) (string, error) {
	b, err := Marshal(v)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(b)
	return hex.EncodeToString(digest[:]), nil
}
