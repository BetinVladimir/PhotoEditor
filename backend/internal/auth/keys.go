package auth

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
)

type FileKeys struct {
	ActiveJWT  string            `json:"active_jwt"`
	JWTSeed    string            `json:"jwt_seed"`
	JWTPublic  map[string]string `json:"jwt_public"`
	OTPPepper  string            `json:"otp_pepper"`
	ActiveMail string            `json:"active_mail"`
	Mail       map[string]string `json:"mail"`
}

// LoadKeys never generates replacement keys on startup: losing a key must be an
// explicit operational event, not silently invalidated sessions or unreadable mail.
func LoadKeys(path, issuer, audience string) (TokenSigner, KeyRing, []byte, error) {
	invalid := errors.New("auth key file is invalid or has unsafe permissions")
	info, err := os.Stat(path)
	if err != nil {
		return TokenSigner{}, KeyRing{}, nil, invalid
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 65536 {
		return TokenSigner{}, KeyRing{}, nil, invalid
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return TokenSigner{}, KeyRing{}, nil, invalid
	}
	var f FileKeys
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(&f) != nil {
		return TokenSigner{}, KeyRing{}, nil, invalid
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return TokenSigner{}, KeyRing{}, nil, invalid
	}
	decode := func(v string) ([]byte, error) {
		b, e := base64.StdEncoding.DecodeString(v)
		if e != nil || len(b) != 32 {
			return nil, invalid
		}
		return b, nil
	}
	seed, err := decode(f.JWTSeed)
	if err != nil {
		return TokenSigner{}, KeyRing{}, nil, invalid
	}
	pepper, err := decode(f.OTPPepper)
	if err != nil {
		return TokenSigner{}, KeyRing{}, nil, invalid
	}
	signer := TokenSigner{Active: f.ActiveJWT, Private: ed25519.NewKeyFromSeed(seed), Public: map[string]ed25519.PublicKey{}, Issuer: issuer, Audience: audience}
	for kid, encoded := range f.JWTPublic {
		b, e := decode(encoded)
		if e != nil || kid == "" {
			return TokenSigner{}, KeyRing{}, nil, invalid
		}
		signer.Public[kid] = ed25519.PublicKey(b)
	}
	if f.ActiveJWT == "" || !bytes.Equal(signer.Public[f.ActiveJWT], signer.Private.Public().(ed25519.PublicKey)) {
		return TokenSigner{}, KeyRing{}, nil, invalid
	}
	ring := KeyRing{Active: f.ActiveMail, Keys: map[string][]byte{}}
	for kid, encoded := range f.Mail {
		b, e := decode(encoded)
		if e != nil || kid == "" {
			return TokenSigner{}, KeyRing{}, nil, invalid
		}
		ring.Keys[kid] = b
	}
	if len(ring.Keys[f.ActiveMail]) != 32 {
		return TokenSigner{}, KeyRing{}, nil, invalid
	}
	return signer, ring, pepper, nil
}

// LoadMailKeys gives the mail worker only the payload decryption keys, without
// JWT signing material or OTP HMAC peppers.
func LoadMailKeys(path string) (KeyRing, error) {
	invalid := errors.New("mail key file is invalid or has unsafe permissions")
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 65536 {
		return KeyRing{}, invalid
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return KeyRing{}, invalid
	}
	var f struct {
		Active string            `json:"active_mail"`
		Mail   map[string]string `json:"mail"`
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(&f) != nil {
		return KeyRing{}, invalid
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return KeyRing{}, invalid
	}
	ring := KeyRing{Active: f.Active, Keys: map[string][]byte{}}
	for kid, encoded := range f.Mail {
		key, e := base64.StdEncoding.DecodeString(encoded)
		if e != nil || len(key) != 32 || kid == "" {
			return KeyRing{}, invalid
		}
		ring.Keys[kid] = key
	}
	if len(ring.Keys[ring.Active]) != 32 {
		return KeyRing{}, invalid
	}
	return ring, nil
}
