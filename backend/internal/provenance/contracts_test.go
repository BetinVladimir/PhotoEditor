package provenance

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func contractBytes(t *testing.T, value any) ([]byte, v2input.Ref) {
	t.Helper()
	raw, err := canonical.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(raw)
	return raw, v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(hash[:])}
}
func TestSigningPolicyRequiresPinnedCompleteOperatorConfiguration(t *testing.T) {
	sha := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	policy := SigningPolicy{Schema: 1, ID: uuid.New(), Revision: "1", SDKLockSHA: sha, KeyHandle: "kms:operator-key", CertificateChainSHA: sha, TimestampMode: "off", AllowedMIME: []string{"image/jpeg", "image/png"}, TrustBundleSHA: sha, RevocationMode: "required"}
	for _, mode := range []string{"valid", "timestamp", "wrong_id", "wrong_hash", "sdk", "trust", "certificate", "revision", "duplicate_mime", "mime", "key", "missing_timestamp", "off_timestamp", "revocation", "unknown", "missing"} {
		t.Run(mode, func(t *testing.T) {
			p := policy
			service := "trusted-timestamp-service"
			switch mode {
			case "timestamp":
				p.TimestampMode = "required"
				p.TimestampServiceID = &service
			case "sdk":
				p.SDKLockSHA = "invalid"
			case "trust":
				p.TrustBundleSHA = ""
			case "certificate":
				p.CertificateChainSHA = "invalid"
			case "revision":
				p.Revision = "01"
			case "duplicate_mime":
				p.AllowedMIME = []string{"image/jpeg", "image/jpeg"}
			case "mime":
				p.AllowedMIME = []string{"image/tiff"}
			case "key":
				p.KeyHandle = "\nsecret"
			case "missing_timestamp":
				p.TimestampMode = "required"
			case "off_timestamp":
				p.TimestampServiceID = &service
			case "revocation":
				p.RevocationMode = "ignore"
			}
			var value any = p
			if mode == "unknown" || mode == "missing" {
				body, _ := canonical.Marshal(p)
				var fields map[string]any
				if err := json.Unmarshal(body, &fields); err != nil {
					t.Fatal(err)
				}
				if mode == "unknown" {
					fields["unexpected"] = true
				} else {
					delete(fields, "revocation_mode")
				}
				value = fields
			}
			raw, reference := contractBytes(t, value)
			id := p.ID
			if mode == "wrong_id" {
				id = uuid.New()
			}
			if mode == "wrong_hash" {
				reference.SHA256 = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
			}
			got, err := DecodeSigningPolicy(raw, reference, id)
			if mode == "valid" || mode == "timestamp" {
				if err != nil || got.ID != policy.ID {
					t.Fatal("valid policy rejected", err)
				}
			} else if err == nil {
				t.Fatal("invalid policy accepted")
			}
		})
	}
}
func TestSigningLineagePreservesOrderedActionsAndGenerationClaims(t *testing.T) {
	sha := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	review := uuid.New()
	lineage := SigningLineage{Schema: 1, SourceHashes: []string{sha}, Actions: []SigningAction{{Operation: "render.encode", Version: "1", ParamsSHA: sha, Generated: true, ReviewID: &review}}}
	raw, reference := contractBytes(t, lineage)
	got, err := DecodeSigningLineage(raw, reference)
	if err != nil || len(got.Actions) != 1 || !got.Actions[0].Generated || *got.Actions[0].ReviewID != review {
		t.Fatal("lineage claims lost", err)
	}
	for _, mode := range []string{"empty_sources", "nil_actions", "bad_hash", "bad_model", "bad_review", "unknown_version"} {
		changed := lineage
		changed.Actions = append([]SigningAction(nil), lineage.Actions...)
		switch mode {
		case "empty_sources":
			changed.SourceHashes = []string{}
		case "nil_actions":
			changed.Actions = nil
		case "bad_hash":
			changed.SourceHashes = []string{"bad"}
		case "bad_model":
			bad := "bad"
			changed.Actions[0].ModelLockSHA = &bad
		case "bad_review":
			zero := uuid.Nil
			changed.Actions[0].ReviewID = &zero
		case "unknown_version":
			changed.Schema = 2
		}
		body, ref := contractBytes(t, changed)
		if _, err := DecodeSigningLineage(body, ref); err == nil {
			t.Fatal("invalid lineage accepted", mode)
		}
	}
}

func TestSigningParamsRejectAliasedInputRoles(t *testing.T) {
	_, params, _ := signingRegistryFixture(t)
	for _, pair := range []string{"export_lineage", "export_policy", "lineage_policy"} {
		candidate := params
		switch pair {
		case "export_lineage":
			candidate.Lineage.ArtifactID = candidate.Export.ArtifactID
		case "export_policy":
			candidate.Policy.ArtifactID = candidate.Export.ArtifactID
		case "lineage_policy":
			candidate.Policy.ArtifactID = candidate.Lineage.ArtifactID
		}
		raw, _ := contractBytes(t, candidate)
		if _, err := DecodeSignParams(raw); err == nil {
			t.Fatal("aliased signing roles accepted", pair)
		}
	}
	raw, _ := contractBytes(t, params)
	if _, err := DecodeSignParams(raw); err != nil {
		t.Fatal("distinct input roles rejected", err)
	}
}
