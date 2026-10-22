package interchange

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

func TestSchedulerExportStyleBackgroundFactoryUsesClaimScope(t *testing.T) {
	tenant, who, intent, frozen, _ := exportExecutionFixture(t)
	claim := ExportAdoptionClaim{
		Tenant: tenant, Workflow: intent.Identity.Workflow, Run: intent.Identity.Run,
		Task: intent.Identity.Task, Result: uuid.New(), Actor: who.UserID,
		Token: uuid.New(), Key: "pipeline-key", RequestSHA: intent.RequestSHA,
		Request: intent.Request,
	}
	if err := claim.validate(); err != nil {
		t.Fatal(err)
	}
	scratch, err := NewExportArchiveScratch(t.TempDir(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	objects := &exportCompositionObjects{}
	images := NativeExportImageQualifier{
		Objects: objects, Scratch: scratch, Probe: &exportStyleProbeStub{},
		RuntimeSHA: rawSHA([]byte("image runtime")),
	}
	catalog := SQLSchedulerExportInputCatalog{
		Claims: SQLExportAdoptionClaimStore{DB: &pgxpool.Pool{}},
		Claim:  claim, Bucket: "photos",
	}
	factory := SQLExportStyleBackgroundFactory{}
	authority, err := factory.BuildExportStyleBackgroundAuthority(context.Background(),
		intent, frozen, catalog, objects, images)
	if err != nil {
		t.Fatal(err)
	}
	scheduler, ok := authority.(SQLSchedulerExportStyleBackgroundAuthority)
	if !ok || scheduler.Catalog.Claim != claim || scheduler.Images.Scratch != scratch {
		t.Fatal("scheduler background not bound to exact claim")
	}
	foreign := catalog
	foreign.Claim.Token = uuid.Nil
	if result, err := factory.BuildExportStyleBackgroundAuthority(context.Background(),
		intent, frozen, foreign, objects, images); err == nil || result != nil {
		t.Fatal("missing claim lease accepted")
	}
	foreign = catalog
	foreign.Claim.Request.Format = "photoeditor_bundle"
	if result, err := factory.BuildExportStyleBackgroundAuthority(context.Background(),
		intent, frozen, foreign, objects, images); err == nil || result != nil {
		t.Fatal("changed claim request accepted")
	}
	if _, err := catalog.LookupExportStyleBackground(context.Background(), v2draft.FrozenBackgroundAsset{}); err != v2input.ErrInvalid {
		t.Fatal("invalid pin reached SQL", err)
	}
	if err := (SQLSchedulerExportStyleBackgroundAuthority{}).QualifyExportStyleBackground(
		context.Background(), v2draft.FrozenBackgroundAsset{}); err != v2input.ErrInvalid {
		t.Fatal("empty native authority accepted", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if result, err := factory.BuildExportStyleBackgroundAuthority(ctx,
		intent, frozen, catalog, objects, images); err != context.Canceled || result != nil {
		t.Fatal("cancelled factory proceeded", err)
	}
}
