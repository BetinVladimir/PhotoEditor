package provenance

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

var ErrInvalid = errors.New("invalid provenance contract")

type SigningPolicy struct {
	Schema              int       `json:"schema_version"`
	ID                  uuid.UUID `json:"id"`
	Revision            string    `json:"revision"`
	SDKLockSHA          string    `json:"sdk_lock_sha"`
	KeyHandle           string    `json:"key_handle"`
	CertificateChainSHA string    `json:"certificate_chain_sha"`
	TimestampMode       string    `json:"timestamp_mode"`
	TimestampServiceID  *string   `json:"timestamp_service_id"`
	AllowedMIME         []string  `json:"allowed_mime"`
	TrustBundleSHA      string    `json:"trust_bundle_sha"`
	RevocationMode      string    `json:"revocation_mode"`
}
type SigningAction struct {
	Operation    string     `json:"operation"`
	Version      string     `json:"version"`
	ParamsSHA    string     `json:"params_sha"`
	ModelLockSHA *string    `json:"model_lock_sha"`
	Generated    bool       `json:"generated"`
	ReviewID     *uuid.UUID `json:"review_id"`
}
type SigningLineage struct {
	Schema       int             `json:"schema_version"`
	SourceHashes []string        `json:"source_hashes"`
	Actions      []SigningAction `json:"actions"`
}
type SignParams struct {
	Schema   int         `json:"schema_version"`
	Export   v2input.Ref `json:"export"`
	Lineage  v2input.Ref `json:"lineage"`
	PolicyID uuid.UUID   `json:"signing_policy_id"`
	Policy   v2input.Ref `json:"policy"`
}
type SignedResult struct {
	Schema   int         `json:"schema_version"`
	Export   v2input.Ref `json:"signed_export"`
	Manifest v2input.Ref `json:"manifest"`
}

func digest(s string) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == 32 && hex.EncodeToString(b) == s
}
func ref(r v2input.Ref) bool { return r.ArtifactID != uuid.Nil && digest(r.SHA256) }
func text(s string, max int) bool {
	return utf8.ValidString(s) && len(s) > 0 && len(s) <= max && strings.TrimSpace(s) == s && !strings.ContainsAny(s, "\x00\r\n")
}
func pinned(raw []byte, r v2input.Ref, max int) bool {
	if !ref(r) || len(raw) < 2 || len(raw) > max {
		return false
	}
	sha := sha256.Sum256(raw)
	return hex.EncodeToString(sha[:]) == r.SHA256
}

// Re-encoding also rejects missing fields, duplicate keys and explicit nulls
// that would otherwise silently become zero-valued required scalar fields.
func decode(raw []byte, out any, max int) error {
	if len(raw) < 2 || len(raw) > max {
		return ErrInvalid
	}
	normalized, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(raw, normalized) {
		return ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil || d.Decode(new(any)) != io.EOF {
		return ErrInvalid
	}
	encoded, err := canonical.Marshal(out)
	if err != nil || !bytes.Equal(raw, encoded) {
		return ErrInvalid
	}
	return nil
}
func DecodeSigningPolicy(raw []byte, policyRef v2input.Ref, policyID uuid.UUID) (SigningPolicy, error) {
	var p SigningPolicy
	if !pinned(raw, policyRef, 1<<20) || decode(raw, &p, 1<<20) != nil {
		return SigningPolicy{}, ErrInvalid
	}
	revision, err := strconv.ParseInt(p.Revision, 10, 64)
	if p.Schema != 1 || p.ID == uuid.Nil || p.ID != policyID || err != nil || revision < 1 || strconv.FormatInt(revision, 10) != p.Revision || !digest(p.SDKLockSHA) || !digest(p.CertificateChainSHA) || !digest(p.TrustBundleSHA) || !text(p.KeyHandle, 200) || len(p.AllowedMIME) < 1 || len(p.AllowedMIME) > 2 {
		return SigningPolicy{}, ErrInvalid
	}
	if p.TimestampMode != "off" && p.TimestampMode != "required" || p.RevocationMode != "off" && p.RevocationMode != "required" {
		return SigningPolicy{}, ErrInvalid
	}
	if p.TimestampMode == "off" && p.TimestampServiceID != nil || p.TimestampMode == "required" && (p.TimestampServiceID == nil || !text(*p.TimestampServiceID, 120)) {
		return SigningPolicy{}, ErrInvalid
	}
	seen := map[string]bool{}
	for _, mime := range p.AllowedMIME {
		if mime != "image/jpeg" && mime != "image/png" || seen[mime] {
			return SigningPolicy{}, ErrInvalid
		}
		seen[mime] = true
	}
	return p, nil
}
func DecodeSigningLineage(raw []byte, lineageRef v2input.Ref) (SigningLineage, error) {
	var l SigningLineage
	if !pinned(raw, lineageRef, 8<<20) || decode(raw, &l, 8<<20) != nil || l.Schema != 1 || len(l.SourceHashes) < 1 || len(l.SourceHashes) > 5000 || l.Actions == nil || len(l.Actions) > 5000 {
		return SigningLineage{}, ErrInvalid
	}
	for _, sha := range l.SourceHashes {
		if !digest(sha) {
			return SigningLineage{}, ErrInvalid
		}
	}
	for _, a := range l.Actions {
		if !text(a.Operation, 80) || !text(a.Version, 20) || !digest(a.ParamsSHA) || a.ModelLockSHA != nil && !digest(*a.ModelLockSHA) || a.ReviewID != nil && *a.ReviewID == uuid.Nil {
			return SigningLineage{}, ErrInvalid
		}
	}
	return l, nil
}

// Used by both wire decoding and trusted in-memory preparation. Typed callers
// must not bypass input-role separation before invoking the native authority.
func validSignParams(p SignParams) bool {
	return p.Schema == 1 && p.PolicyID != uuid.Nil && ref(p.Export) && ref(p.Lineage) && ref(p.Policy) && p.Export.ArtifactID != p.Lineage.ArtifactID && p.Export.ArtifactID != p.Policy.ArtifactID && p.Lineage.ArtifactID != p.Policy.ArtifactID
}

func DecodeSignParams(raw []byte) (SignParams, error) {
	var p SignParams
	if decode(raw, &p, 1<<20) != nil || !validSignParams(p) {
		return SignParams{}, ErrInvalid
	}
	return p, nil
}
func DecodeSignedResult(raw []byte) (SignedResult, error) {
	var r SignedResult
	if decode(raw, &r, 1<<20) != nil || r.Schema != 1 || !ref(r.Export) || !ref(r.Manifest) || r.Export.ArtifactID == r.Manifest.ArtifactID {
		return SignedResult{}, ErrInvalid
	}
	return r, nil
}
