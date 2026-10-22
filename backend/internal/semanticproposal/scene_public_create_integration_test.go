package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
	"strings"
	"testing"
	"time"
)

// Same ordinary production constructor as the existing scene-tail owner, with
// an independent fresh fixture DB and actual public HTTP lifecycle.
func TestIntegrationSceneQualifiedPublicCreate(t *testing.T) {
	raw := os.Getenv("PHOTO_SEMANTIC_INTENT_PUBLISH_CASE")
	if raw == "" {
		t.Skip("isolated migrated DB and versioned object fixture required")
	}
	var f struct {
		Tenant, Actor, Session             uuid.UUID
		Intent                             v2input.SemanticEditIntent
		Objects                            objectstore.Config
		Qualification, PersonQualification v2input.SemanticQualificationPin
		SceneQualifications                [2]v2input.SemanticQualificationPin
		ScenePolicy                        json.RawMessage
		LegacyOrdinary                     bool
		PublicSceneVariant                 string
	}
	if json.Unmarshal([]byte(raw), &f) != nil || f.LegacyOrdinary || len(f.ScenePolicy) == 0 {
		t.Fatal("ordinary scene tail fixture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 19*time.Minute)
	defer cancel()
	owner, err := pgxpool.New(ctx, os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	config, err := pgxpool.ParseConfig(os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.Tracer = newNativeTimingTracer(t)
	config.ConnConfig.RuntimeParams["role"] = "photo_api"
	api, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()
	config = config.Copy()
	config.ConnConfig.RuntimeParams["role"] = "photo_scheduler"
	scheduler, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer scheduler.Close()
	objects, err := objectstore.New(f.Objects)
	if err != nil {
		t.Fatal(err)
	}
	color, err := LoadTransferColorProfile("../../../resources/icc")
	if err != nil {
		t.Fatal(err)
	}
	var project uuid.UUID
	if err = owner.QueryRow(ctx, `SELECT a.project_id FROM photo.asset_versions v JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id WHERE v.tenant_id=$1 AND v.id=$2`, f.Tenant, f.Intent.Reference.AssetVersionID).Scan(&project); err != nil {
		t.Fatal(err)
	}
	who := auth.Identity{UserID: f.Actor, SessionID: f.Session}
	expected := func(operation string, pin v2input.SemanticQualificationPin) v2qualification.Expected {
		return v2qualification.Expected{Operation: operation, Version: "1", Backend: pin.Backend, ModelLockSHA: pin.ModelLockSHA, RuntimeLockSHA: pin.RuntimeLockSHA, DeviceProfileSHA: pin.DeviceProfileSHA, DatasetSHA: pin.DatasetSHA, IssuerID: pin.IssuerID, RequiredChecks: []string{"quality"}, TestFixtureSHAs: map[string]struct{}{strings.Repeat("e", 64): {}}, EvidenceSHAs: map[string]struct{}{strings.Repeat("f", 64): {}}, CalibrationSHAs: map[string]struct{}{pin.Calibration.SHA256: {}}, LicenseCleared: true}
	}
	transferExpected := expected("semantic.transfer", f.Qualification)
	if !f.LegacyOrdinary {
		_, policySHA, err := v2input.DecodeSemanticScenePolicy(f.ScenePolicy)
		if err != nil {
			t.Fatal(err)
		}
		transferExpected.RequiredChecks = []string{"quality", "semantic.scene.farneback_depth_plane_v1"}
		transferExpected.EvidenceSHAs = map[string]struct{}{policySHA: {}}
	}
	producer := func(operation string, pin v2input.SemanticQualificationPin) TransferSceneProducer {
		return TransferSceneProducer{Report: pin.Report, Expected: expected(operation, pin), Costs: v2input.SemanticQualifiedCosts{VRAMBytes: 128 << 20}}
	}
	reader := SQLTransferSourceReader{DB: api, Objects: objects, Bucket: f.Objects.Bucket, ProjectID: project}
	p := TransferDraftPreparer{Reader: reader, Scheduler: scheduler, Documents: v2draft.Publisher{DB: api, Objects: objects, Bucket: f.Objects.Bucket}, TransportDocuments: artifactjson.Publisher{Repository: artifactjson.SQLRepository{DB: scheduler, Bucket: f.Objects.Bucket}, Objects: objects}, ReservedBytes: 128 << 20, Configuration: TransferPreparation{Qualification: f.Qualification, Expected: transferExpected, Color: color, Costs: v2input.SemanticQualifiedCosts{VRAMBytes: 128 << 20}, RAM: 8 << 30, VRAM: 1 << 30, Scratch: 1 << 30, Output: 64 << 20, MetadataBytes: 16 << 20, PersonProtection: &TransferPersonProtection{Report: f.PersonQualification.Report, Expected: expected("mask.person", f.PersonQualification)}, Scene: &TransferScenePreparation{Policy: append(json.RawMessage(nil), f.ScenePolicy...), Scene: producer("scene.parse", f.SceneQualifications[0]), Depth: producer("depth.predict", f.SceneQualifications[1])}}}

	if f.PublicSceneVariant != "ordinary-admission" && f.PublicSceneVariant != "create-service" && f.PublicSceneVariant != "create-person" && f.PublicSceneVariant != "create-person-uncovered" {
		t.Fatal("closed public scene fixture variant")
	}
	deployment := createDeploymentForProbe(t, p)
	service, err := deployment.Build(ctx, api, scheduler, objects, f.Objects.Bucket)
	if err != nil {
		t.Fatal("scene public deployment", err)
	}
	rawIntent, err := json.Marshal(f.Intent)
	if err != nil {
		t.Fatal(err)
	}
	request, err := CompileRequest(f.Tenant, f.Actor, "proposal-publication", rawIntent)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PHOTO_TEST_V27_SCENE_TAIL", "1")
	probeSceneCreateHTTP(t, ctx, service, p, owner, who, request, "proposal-publication", f.PublicSceneVariant == "create-person-uncovered")
}
