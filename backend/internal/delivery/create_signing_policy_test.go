package delivery

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/exportpreset"
)

type createSigningPolicyCheck struct {
	tenant, id uuid.UUID
	sha        string
	calls      int
	err        error
	mimes      []string
}

func (c *createSigningPolicyCheck) CheckDeliveryPolicy(_ context.Context,
	tenant, id uuid.UUID, sha string) error {
	c.calls++
	c.tenant, c.id, c.sha = tenant, id, sha
	return c.err
}
func (c *createSigningPolicyCheck) CheckDeliveryPolicyMIMEs(_ context.Context, tenant, id uuid.UUID, sha string, mimes []string) error {
	c.calls++
	c.tenant, c.id, c.sha = tenant, id, sha
	c.mimes = append([]string(nil), mimes...)
	return c.err
}

func TestCreateRequiredSigningPolicyUsesInstalledTenantChecker(t *testing.T) {
	tenant, id := uuid.New(), uuid.New()
	policy := Policy{Provenance: "required", SigningPolicyID: id,
		SigningPolicySHA: strings.Repeat("a", 64)}
	if err := checkCreateSigningPolicy(context.Background(), tenant, policy, nil); err == nil {
		t.Fatal("required package accepted without installed signer")
	}
	checker := &createSigningPolicyCheck{}
	if err := checkCreateSigningPolicy(context.Background(), tenant, policy, checker); err != nil ||
		checker.calls != 1 || checker.tenant != tenant || checker.id != id ||
		checker.sha != policy.SigningPolicySHA {
		t.Fatal("frozen signing choice not checked", err)
	}
	checker.err = errors.New("operator unavailable")
	if err := checkCreateSigningPolicy(context.Background(), tenant, policy, checker); err == nil {
		t.Fatal("unavailable operator accepted")
	}
	checker.calls = 0
	if err := checkCreateSigningPolicy(context.Background(), tenant, Policy{Provenance: "off"}, checker); err != nil || checker.calls != 0 {
		t.Fatal("off policy invoked signing authority", err)
	}
}

func TestCreateSigningChoiceChecksActualFrozenPresetMIME(t *testing.T) {
	_, _, _, configs := freezeFixture(t)
	catalog := CreateCatalog{Presets: []CreateCatalogPreset{{ID: configs[0].ID, SHA256: configs[0].SHA, ConfigJCS: configs[0].Raw}}}
	tenant, id := uuid.New(), uuid.New()
	policy := Policy{Provenance: "required", SigningPolicyID: id, SigningPolicySHA: strings.Repeat("a", 64), RequiredPresets: []uuid.UUID{configs[0].ID}}
	checker := &createSigningPolicyCheck{}
	if err := checkCreateSigningMIMEs(context.Background(), tenant, policy, catalog, checker); err != nil || checker.calls != 1 || checker.tenant != tenant || checker.id != id || checker.sha != policy.SigningPolicySHA || len(checker.mimes) != 1 || checker.mimes[0] != "image/jpeg" {
		t.Fatal("actual preset MIME not checked", checker, err)
	}
	checker.calls = 0
	if err := checkCreateSigningMIMEs(context.Background(), tenant, Policy{Provenance: "off"}, catalog, checker); err != nil || checker.calls != 0 {
		t.Fatal("off policy checked MIME", err)
	}
	catalog.Presets[0].SHA256 = strings.Repeat("b", 64)
	if err := checkCreateSigningMIMEs(context.Background(), tenant, policy, catalog, checker); err == nil || checker.calls != 0 {
		t.Fatal("substituted config accepted", err)
	}
}

func TestCreateSigningChoiceChecksMixedFrozenPresetMIMEs(t *testing.T) {
	_, _, _, configs := freezeFixture(t)
	png, err := canonical.Marshal(exportpreset.Config{Format: "png", ICCID: "srgb_v1", Subsampling: "444", Alpha: "preserve", MetadataPolicy: "strip", NamingTemplate: "{name}"})
	if err != nil {
		t.Fatal(err)
	}
	catalog := CreateCatalog{Presets: []CreateCatalogPreset{
		{ID: uuid.New(), SHA256: digest(png), ConfigJCS: png},
		{ID: configs[0].ID, SHA256: configs[0].SHA, ConfigJCS: configs[0].Raw},
	}}
	checker := &createSigningPolicyCheck{}
	policy := Policy{Provenance: "required", SigningPolicyID: uuid.New(), SigningPolicySHA: strings.Repeat("a", 64), RequiredPresets: []uuid.UUID{configs[0].ID}, OptionalPresets: []uuid.UUID{catalog.Presets[0].ID}}
	if err := checkCreateSigningMIMEs(context.Background(), uuid.New(), policy, catalog, checker); err != nil || checker.calls != 1 || strings.Join(checker.mimes, ",") != "image/jpeg,image/png" {
		t.Fatal("mixed actual MIME set not checked", checker.mimes, err)
	}
	checker.err = errors.New("installed policy excludes PNG")
	if err := checkCreateSigningMIMEs(context.Background(), uuid.New(), policy, catalog, checker); err == nil {
		t.Fatal("unsupported PNG was accepted")
	}
	checker.calls = 0
	catalog.Presets[0].ConfigJCS = []byte("{}")
	if err := checkCreateSigningMIMEs(context.Background(), uuid.New(), policy, catalog, checker); err == nil || checker.calls != 0 {
		t.Fatal("substituted PNG config reached signing checker", err)
	}
	catalog.Presets[0].ConfigJCS = png
	catalog.Presets[0].ID = uuid.New()
	if err := checkCreateSigningMIMEs(context.Background(), uuid.New(), policy, catalog, checker); err == nil || checker.calls != 0 {
		t.Fatal("unselected frozen preset reached signing checker", err)
	}
}

func TestCreateSigningChoiceChecksWatermarkedFrozenPresetMIME(t *testing.T) {
	quality := 90
	id := uuid.New()
	raw, err := canonical.Marshal(exportpreset.Config{
		Format: "jpeg", ICCID: "srgb_v1", Quality: &quality, Subsampling: "444",
		Alpha: "matte", MatteRGB: []float64{1, 1, 1}, MetadataPolicy: "strip",
		NamingTemplate: "{name}", Watermark: &exportpreset.Watermark{AssetID: uuid.New(), Opacity: 0.5, Anchor: "center"},
	})
	if err != nil {
		t.Fatal(err)
	}
	policy := Policy{Provenance: "required", RequiredPresets: []uuid.UUID{id}, SigningPolicyID: uuid.New(), SigningPolicySHA: strings.Repeat("a", 64)}
	catalog := CreateCatalog{Presets: []CreateCatalogPreset{{ID: id, SHA256: digest(raw), ConfigJCS: raw}}}
	checker := &createSigningPolicyCheck{}
	if err := checkCreateSigningMIMEs(context.Background(), uuid.New(), policy, catalog, checker); err != nil || checker.calls != 1 || strings.Join(checker.mimes, ",") != "image/jpeg" {
		t.Fatal("watermarked JPEG bypassed actual MIME qualification", checker.mimes, err)
	}
}
