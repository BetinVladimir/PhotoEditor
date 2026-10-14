package interchange

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/v2input"
)

func TestAPIExportBackgroundFactoryBindsCurrentRequestScope(t *testing.T) {
	tenant, who, intent, frozen, _ := exportExecutionFixture(t)
	objects := &exportCompositionObjects{}
	scratch, err := NewExportArchiveScratch(t.TempDir(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	style := ExportStyleValidatorInstallation{
		BackgroundFactory: SQLAPIExportStyleBackgroundFactory{},
		Scratch:           scratch, ImageProbe: &exportStyleProbeStub{},
		ImageRuntimeSHA: rawSHA([]byte("image runtime")),
	}
	catalog := SQLExportDocumentCatalog{
		DB: &pgxpool.Pool{}, Tenant: tenant, Who: who,
		Request: intent.Request, Key: "pipeline-key", Bucket: "photos",
	}
	background, err := style.BuildScopedExportBackground(context.Background(),
		intent, frozen, catalog, objects)
	if err != nil {
		t.Fatal(err)
	}
	scoped, ok := background.(SQLExportStyleBackgroundAuthority)
	if !ok || scoped.Tenant != tenant || scoped.Project != intent.Request.ProjectID ||
		scoped.Who != who || scoped.Resolver.DB != catalog.DB ||
		scoped.Resolver.Bucket != catalog.Bucket {
		t.Fatal("background authority not bound to selected export")
	}
	foreign := catalog
	foreign.Request.ProjectID = intent.Identity.Run
	if authority, err := style.BuildScopedExportBackground(context.Background(),
		intent, frozen, foreign, objects); err == nil || authority != nil {
		t.Fatal("foreign project accepted")
	}
	if authority, err := style.BuildScopedExportBackground(context.Background(),
		intent, frozen, SQLSchedulerExportInputCatalog{}, objects); err == nil || authority != nil {
		t.Fatal("API session authority used for scheduler")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if authority, err := style.BuildScopedExportBackground(ctx,
		intent, frozen, catalog, objects); err != context.Canceled || authority != nil {
		t.Fatal("cancelled background factory ran", err)
	}
	style.ImageRuntimeSHA = "wrong"
	if authority, err := style.BuildScopedExportBackground(context.Background(),
		intent, frozen, catalog, objects); err != v2input.ErrInvalid || authority != nil {
		t.Fatal("unqualified image runtime accepted", err)
	}
}
