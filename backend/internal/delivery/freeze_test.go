package delivery

import (
	"bytes"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/exportpreset"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestCompileFrozenInputAcceptsMatchingMetadataModes(t *testing.T) {
	for _, mode := range []string{"copyright", "approved_exif"} {
		t.Run(mode, func(t *testing.T) {
			ref, raw, policy, configs := freezeFixture(t)
			var config exportpreset.Config
			if err := json.Unmarshal(configs[0].Raw, &config); err != nil {
				t.Fatal(err)
			}
			config.MetadataPolicy = mode
			configRaw, err := canonical.Marshal(config)
			if err != nil {
				t.Fatal(err)
			}
			configs[0].Raw = configRaw
			configs[0].SHA = digest(configRaw)
			configs[0].Ref.SHA256 = configs[0].SHA
			policy.MetadataPolicy = mode
			if mode == "approved_exif" {
				policy.ApprovedFields = []string{"author"}
			}
			frozen, _, _, err := CompileFrozenInput(ref, raw, policy, configs)
			if err != nil || frozen.Policy.MetadataPolicy != mode || frozen.Presets[0].PresetSHA != configs[0].SHA {
				t.Fatal("matching frozen metadata mode refused", mode, err)
			}
		})
	}
}

func freezeFixture(t *testing.T) (v2input.Ref, []byte, Policy, []PresetContent) {
	t.Helper()
	source := v2input.Ref{ArtifactID: uuid.New(), SHA256: digest([]byte("source"))}
	_, snapshot, err := v2input.CompilePinnedSessionInputSnapshot(uuid.New(), []v2input.SessionSnapshotItem{{Ordinal: 0, Binding: v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: source, CanvasHash: digest([]byte("canvas"))}, WorkingImage: source, Recipe: v2input.Ref{ArtifactID: uuid.New(), SHA256: digest([]byte("recipe"))}}})
	if err != nil {
		t.Fatal(err)
	}
	q := 90
	config, err := canonical.Marshal(exportpreset.Config{Format: "jpeg", ICCID: "srgb_v1", Quality: &q, Subsampling: "444", Alpha: "matte", MatteRGB: []float64{1, 1, 1}, MetadataPolicy: "strip", NamingTemplate: "{name}"})
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	hash := digest(config)
	return v2input.Ref{ArtifactID: uuid.New(), SHA256: digest(snapshot)}, snapshot, Policy{RequiredPresets: []uuid.UUID{id}, OptionalPresets: []uuid.UUID{}, MetadataPolicy: "strip", Provenance: "off", RequireReview: true}, []PresetContent{{ID: id, SHA: hash, Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: hash}, Raw: config}}
}
func TestCompileFrozenInputDeterministicAndOwned(t *testing.T) {
	ref, raw, policy, configs := freezeFixture(t)
	first, body, sha, err := CompileFrozenInput(ref, raw, policy, configs)
	if err != nil {
		t.Fatal(err)
	}
	_, again, hash, err := CompileFrozenInput(ref, raw, policy, configs)
	if err != nil || !bytes.Equal(body, again) || sha != hash {
		t.Fatal("unstable document")
	}
	policy.RequiredPresets[0] = uuid.New()
	configs[0].ID = uuid.New()
	if first.Policy.RequiredPresets[0] != first.Presets[0].PresetID {
		t.Fatal("borrowed input")
	}
}
func TestCompileFrozenInputRefusesIncompleteOrChangedInventory(t *testing.T) {
	for _, kind := range []string{"snapshot", "missing", "foreign", "duplicate", "config", "provenance", "metadata"} {
		t.Run(kind, func(t *testing.T) {
			ref, raw, policy, configs := freezeFixture(t)
			switch kind {
			case "snapshot":
				raw = append(raw, ' ')
			case "missing":
				configs = nil
			case "foreign":
				configs[0].ID = uuid.New()
			case "duplicate":
				policy.OptionalPresets = policy.RequiredPresets
			case "config":
				configs[0].Raw = []byte("{}")
			case "provenance":
				policy.Provenance = "required"
			case "metadata":
				policy.MetadataPolicy = "copyright"
			}
			if _, _, _, err := CompileFrozenInput(ref, raw, policy, configs); err == nil {
				t.Fatal("invalid inventory accepted")
			}
		})
	}
}

func TestFrozenDeliverySigningPolicyChoiceIsExplicit(t *testing.T) {
	ref, raw, policy, configs := freezeFixture(t)
	policy.SigningPolicyID = uuid.New()
	policy.SigningPolicySHA = strings.Repeat("a", 64)
	if _, _, _, err := CompileFrozenInput(ref, raw, policy, configs); err == nil {
		t.Fatal("off policy carried signer choice")
	}
	policy.Provenance = "required"
	if !validDeliverySigningPolicyChoice(policy) {
		t.Fatal("valid required signing choice rejected")
	}
	if frozen, _, _, err := CompileFrozenInput(ref, raw, policy, configs); err != nil || frozen.Policy.SigningPolicyID != policy.SigningPolicyID {
		t.Fatal("required choice not frozen", err)
	}
	policy.SigningPolicySHA = ""
	if validDeliverySigningPolicyChoice(policy) {
		t.Fatal("unfrozen signer choice accepted")
	}
}
