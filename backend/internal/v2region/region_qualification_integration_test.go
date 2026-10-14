package v2region

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/canonical"
)

func TestIntegrationRegionQualificationCompiler(t *testing.T) {
	raw := os.Getenv("PHOTO_REGION_QUALIFICATION_DATABASE_URL")
	if raw == "" {
		t.Skip("isolated migrated database required")
	}
	var ids struct{ Tenant, Project uuid.UUID }
	if json.Unmarshal([]byte(os.Getenv("PHOTO_REGION_QUALIFICATION_CASE")), &ids) != nil || ids.Tenant == uuid.Nil || ids.Project == uuid.Nil {
		t.Fatal("missing case")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	config, err := pgxpool.ParseConfig(raw)
	if err != nil {
		t.Fatal("invalid isolated database config")
	}
	config.ConnConfig.RuntimeParams["role"] = "photo_api"
	api, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal("isolated database unavailable")
	}
	defer api.Close()
	plan, _, _, _ := regionNodeFixture(t)
	doc, err := plan.decode()
	if err != nil {
		t.Fatal(err)
	}
	doc.TenantID = ids.Tenant
	doc.ProjectID = ids.Project
	plan.body, err = canonical.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	plan.digest, err = canonical.Hash(doc)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := pgxpool.New(ctx, raw)
	if err != nil {
		t.Fatal("isolated administrator unavailable")
	}
	defer admin.Close()
	profile := regionCalibrationFixture(t, plan)
	profile.Qualification = RegionQualification{RuntimeSHA: strings.Repeat("b", 64), ModelSHA: regionSHA, ICCSHA: regionSHA,
		DeviceSHA: strings.Repeat("c", 64), DatasetSHA: strings.Repeat("d", 64), PolicySHA: strings.Repeat("2", 64), Backend: "cuda"}
	profile.EvidenceSHA = strings.Repeat("f", 64)
	manifest, pin := regionCalibrationBytes(t, []RegionCalibration{profile})
	catalog, err := NewPinnedRegionCalibrationCatalog(manifest, pin)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, "UPDATE photo.v2_qualification_policies SET calibration_shas=ARRAY[$1]::text[] WHERE operation='region.preview'", pin); err != nil {
		t.Fatal(err)
	}
	resolver := SQLRegionQualificationResolver{API: api, CalibrationSHA: pin, EvidenceSHA: profile.EvidenceSHA}
	q, err := resolver.ResolveRegionQualification(ctx, plan)
	if err != nil || q != profile.Qualification {
		t.Fatal("live qualification", q, err)
	}
	provider, err := NewSQLRegionProfileProvider(api, catalog, profile.EvidenceSHA)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := provider.CompileRegionProfile(ctx, plan)
	if err != nil || policy.PolicySHA != q.PolicySHA || policy.ReservedBytes != 24*1024*1024+400 {
		t.Fatal("live compiler", policy, err)
	}
	checkGate := func(raw []byte, want bool) {
		t.Helper()
		tx, err := admin.BeginTx(ctx, pgx.TxOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		if _, err = tx.Exec(ctx, "SET LOCAL ROLE photo_fn_owner"); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", ids.Tenant.String()); err != nil {
			t.Fatal(err)
		}
		var ready bool
		err = tx.QueryRow(ctx, "SELECT photo.v26_region_qualification_pin_ready($1,$2,$3,$4::jsonb)", ids.Tenant, ids.Project, policy.PolicySHA, string(raw)).Scan(&ready)
		if err != nil || ready != want {
			t.Fatal("live pin gate", ready, want, err)
		}
	}
	checkGate(policy.Profile, true)
	var invalid map[string]any
	json.Unmarshal(policy.Profile, &invalid)
	invalid["region_qualification"].(map[string]any)["catalog_sha"] = strings.Repeat("0", 64)
	invalidRaw, _ := canonical.Marshal(invalid)
	checkGate(invalidRaw, false)
	json.Unmarshal(policy.Profile, &invalid)
	delete(invalid["region_qualification"].(map[string]any), "sample")
	invalidRaw, _ = canonical.Marshal(invalid)
	checkGate(invalidRaw, false)
	// Selection remains unavailable when deployment calibration is no longer allowed.
	stale := resolver
	stale.CalibrationSHA = strings.Repeat("0", 64)
	if _, err := stale.ResolveRegionQualification(ctx, plan); !errors.Is(err, ErrRegionUnavailable) {
		t.Fatal("unapproved calibration", err)
	}
	wrongRole := resolver
	wrongRole.API = admin
	if _, err := wrongRole.ResolveRegionQualification(ctx, plan); !errors.Is(err, ErrRegionUnavailable) {
		t.Fatal("maintenance context accepted", err)
	}
	if _, err = admin.Exec(ctx, "UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE operation='region.preview'"); err != nil {
		t.Fatal(err)
	}
	checkGate(policy.Profile, false)
	if _, err = provider.CompileRegionProfile(ctx, plan); !errors.Is(err, ErrRegionUnavailable) {
		t.Fatal("revoked live policy accepted", err)
	}
}
