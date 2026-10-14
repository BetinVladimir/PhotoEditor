package semanticproposal

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
	"strings"
	"testing"
	"time"
)

type failManifestOnce struct {
	delegate v2input.DocumentPublisher
	failed   bool
}

func (p *failManifestOnce) Publish(ctx context.Context, scope artifactjson.Scope, body []byte) (artifactjson.Ref, error) {
	if scope.Purpose == "v2_input_manifest" && !p.failed {
		p.failed = true
		return artifactjson.Ref{}, errors.New("injected manifest publication failure")
	}
	return p.delegate.Publish(ctx, scope, body)
}

type afterManifest struct {
	delegate v2input.DocumentPublisher
	after    func(context.Context) error
	done     bool
}

func (p *afterManifest) Publish(ctx context.Context, scope artifactjson.Scope, body []byte) (artifactjson.Ref, error) {
	ref, err := p.delegate.Publish(ctx, scope, body)
	if err == nil && scope.Purpose == "v2_input_manifest" && !p.done {
		p.done = true
		err = p.after(ctx)
	}
	return ref, err
}

func TestIntegrationTransferDraftPreparer(t *testing.T) {
	raw := os.Getenv("PHOTO_SEMANTIC_INTENT_PUBLISH_CASE")
	if raw == "" {
		t.Skip("isolated fixture required")
	}
	var f struct {
		Tenant, Actor, Session                uuid.UUID
		Intent                                v2input.SemanticEditIntent
		Objects                               objectstore.Config
		Protection                            v2input.Ref
		Qualification                         v2input.SemanticQualificationPin
		Revoked, FailManifest, RevokeManifest bool
	}
	if json.Unmarshal([]byte(raw), &f) != nil {
		t.Fatal("fixture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cfg, err := pgxpool.ParseConfig(os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal("database config")
	}
	cfg.ConnConfig.RuntimeParams["role"] = "photo_api"
	api, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()
	cfg = cfg.Copy()
	cfg.ConnConfig.RuntimeParams["role"] = "photo_scheduler"
	scheduler, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer scheduler.Close()
	who := auth.Identity{UserID: f.Actor, SessionID: f.Session}
	body, err := canonical.Marshal(f.Intent)
	if err != nil {
		t.Fatal(err)
	}
	request, err := CompileRequest(f.Tenant, f.Actor, "proposal-publication", body)
	if err != nil {
		t.Fatal(err)
	}
	inventory, err := ReadInventory(ctx, api, who, request)
	if f.Revoked {
		if err == nil {
			t.Fatal("revoked inventory")
		}
		t.Log("V27_PROPOSAL_REVOKED_OK")
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	objects, err := objectstore.New(f.Objects)
	if err != nil {
		t.Fatal(err)
	}
	color, err := LoadTransferColorProfile("../../../resources/icc")
	if err != nil {
		t.Fatal(err)
	}
	q := f.Qualification
	trusted := v2qualification.Expected{Operation: "semantic.transfer", Version: "1", Backend: "cuda", ModelLockSHA: q.ModelLockSHA, RuntimeLockSHA: q.RuntimeLockSHA, DeviceProfileSHA: q.DeviceProfileSHA, DatasetSHA: q.DatasetSHA, IssuerID: q.IssuerID, RequiredChecks: []string{"quality"}, TestFixtureSHAs: map[string]struct{}{strings.Repeat("e", 64): {}}, EvidenceSHAs: map[string]struct{}{strings.Repeat("f", 64): {}}, CalibrationSHAs: map[string]struct{}{q.Calibration.SHA256: {}}, LicenseCleared: true}
	documents := artifactjson.Publisher{Repository: artifactjson.SQLRepository{DB: scheduler, Bucket: f.Objects.Bucket}, Objects: objects}
	var transport v2input.DocumentPublisher = documents
	injected := &failManifestOnce{delegate: documents}
	if f.FailManifest {
		transport = injected
	}
	preparer := TransferDraftPreparer{Reader: SQLTransferSourceReader{DB: api, Objects: objects, Bucket: f.Objects.Bucket, ProjectID: inventory.Scope.ProjectID}, Documents: v2draft.Publisher{DB: api, Objects: objects, Bucket: f.Objects.Bucket}, Scheduler: scheduler, TransportDocuments: transport, Configuration: TransferPreparation{Qualification: q, Expected: trusted, Color: color, RAM: 1 << 30, MetadataBytes: 64 << 20, VRAM: 1 << 30, Scratch: 1 << 30, Output: 1 << 30, SourceProtection: f.Protection, TargetProtection: []v2input.Ref{f.Protection}}, ReservedBytes: 1 << 28}
	inspector, err := pgxpool.New(ctx, os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal("configuration inspection", err)
	}
	defer inspector.Close()
	var beforeArtifacts, afterArtifacts int
	if err := inspector.QueryRow(ctx, "SELECT count(*) FROM photo.artifacts WHERE tenant_id=$1", f.Tenant).Scan(&beforeArtifacts); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*TransferPreparation){
		func(c *TransferPreparation) { c.Expected.RuntimeLockSHA = strings.Repeat("0", 64) },
		func(c *TransferPreparation) { c.Color = TransferColorProfile{} },
		func(c *TransferPreparation) { c.SourceProtection = v2input.Ref{} },
	} {
		invalid := preparer
		change(&invalid.Configuration)
		if result, err := invalid.Prepare(ctx, who, f.Tenant, "proposal-publication", body); err != v2input.ErrInvalid || result.RunID != uuid.Nil {
			t.Fatal("invalid deployment config published", err)
		}
	}
	if err := inspector.QueryRow(ctx, "SELECT count(*) FROM photo.artifacts WHERE tenant_id=$1", f.Tenant).Scan(&afterArtifacts); err != nil || afterArtifacts != beforeArtifacts {
		t.Fatal("invalid config changed artifacts", err)
	}
	t.Log("V27_PREPARER_CONFIG_PREFLIGHT_OK")
	if f.RevokeManifest {
		revokedTransport := &afterManifest{delegate: documents, after: func(ctx context.Context) error {
			_, err := inspector.Exec(ctx, "UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1", f.Session)
			return err
		}}
		preparer.TransportDocuments = revokedTransport
		result, err := preparer.Prepare(ctx, who, f.Tenant, "proposal-publication", body)
		if err != v2input.ErrConflict || result.RunID != uuid.Nil || !revokedTransport.done {
			t.Fatal("revoked registration accepted", err)
		}
		var rows int
		task, _ := v2input.TaskID(request.WorkflowID, "semantic.transfer", 0)
		if err := inspector.QueryRow(ctx, "SELECT count(*) FROM photo.v2_task_inputs WHERE tenant_id=$1 AND task_id=$2", f.Tenant, task).Scan(&rows); err != nil || rows != 0 {
			t.Fatal("revoked registration persisted inputs", rows, err)
		}
		// Restore only this isolated fixture session to exercise same-plan recovery.
		if _, err := inspector.Exec(ctx, "UPDATE photo.sessions SET revoked_at=NULL WHERE id=$1", f.Session); err != nil {
			t.Fatal(err)
		}
		preparer.TransportDocuments = documents
		t.Log("V27_PREPARER_REVOKED_REGISTRATION_OK")
	}

	var retainedChunk v2input.Ref
	if f.FailManifest {
		partial, err := preparer.Prepare(ctx, who, f.Tenant, "proposal-publication", body)
		if err == nil || !strings.Contains(err.Error(), "injected manifest publication failure") || partial.RunID != uuid.Nil || !injected.failed {
			t.Fatal("partial failure returned success", err)
		}
		owner, err := pgxpool.New(ctx, os.Getenv("PHOTO_TEST_DATABASE_URL"))
		if err != nil {
			t.Fatal("partial state inspection", err)
		}
		defer owner.Close()
		var status string
		var sealed bool
		if err := owner.QueryRow(ctx, "SELECT status,graph_sealed FROM photo.runs WHERE tenant_id=$1 AND id=$2", f.Tenant, request.RunID).Scan(&status, &sealed); err != nil || status != "draft" || sealed {
			t.Fatal("partial draft state", status, sealed, err)
		}
		task, _ := v2input.TaskID(request.WorkflowID, "semantic.transfer", 0)
		var inputs, manifests int
		if err := owner.QueryRow(ctx, "SELECT count(*) FROM photo.v2_task_inputs WHERE tenant_id=$1 AND task_id=$2", f.Tenant, task).Scan(&inputs); err != nil || inputs != 0 {
			t.Fatal("partial failure registered inputs", inputs, err)
		}
		if err := owner.QueryRow(ctx, "SELECT count(*) FROM photo.backend_documents WHERE tenant_id=$1 AND run_id=$2 AND purpose='v2_input_manifest'", f.Tenant, request.RunID).Scan(&manifests); err != nil || manifests != 0 {
			t.Fatal("partial manifest persisted", manifests, err)
		}
		if err := owner.QueryRow(ctx, "SELECT artifact_id,sha256 FROM photo.backend_documents WHERE tenant_id=$1 AND run_id=$2 AND purpose='v2_input_chunk_0' AND state='ready'", f.Tenant, request.RunID).Scan(&retainedChunk.ArtifactID, &retainedChunk.SHA256); err != nil {
			t.Fatal("completed chunk missing", err)
		}
		t.Log("V27_PREPARER_PARTIAL_FAILURE_OK")
	}
	result, err := preparer.Prepare(ctx, who, f.Tenant, "proposal-publication", body)
	expected := "created"
	if f.FailManifest || f.RevokeManifest {
		expected = "pending"
	}
	if err != nil || result.RunID != request.RunID || result.Outcome != expected || len(result.transports) != 1 {
		t.Fatal("fresh preparer", result.Outcome, err)
	}
	publishedForRace, err := PublishIntent(ctx, preparer.Documents, who, request)
	if err != nil {
		t.Fatal(err)
	}
	var protectionKey string
	if err := inspector.QueryRow(ctx, "SELECT object_key FROM photo.artifacts WHERE tenant_id=$1 AND id=$2", f.Tenant, f.Protection.ArtifactID).Scan(&protectionKey); err != nil {
		t.Fatal(err)
	}
	qualificationRace := &afterSecondMaskOpen{delegate: objects, key: protectionKey, opens: 1, after: func(ctx context.Context) error {
		_, err := inspector.Exec(ctx, "UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=$1", q.PolicyID)
		return err
	}}
	raceReader := preparer.Reader
	raceReader.Objects = qualificationRace
	raceInputs, raceDocuments, raceErr := raceReader.PrepareTransferInputs(ctx, who, request, publishedForRace, preparer.Configuration)
	if !qualificationRace.done || raceErr != v2input.ErrInvalid || raceInputs != nil || raceDocuments != nil {
		t.Fatal("qualification revoked during later IO accepted", raceErr, qualificationRace.done)
	}
	if _, err := inspector.Exec(ctx, "UPDATE photo.v2_qualification_policies SET admission_enabled=true WHERE id=$1", q.PolicyID); err != nil {
		t.Fatal(err)
	}
	t.Log("V27_PREPARER_QUALIFICATION_LATE_IO_RACE_OK")
	if len(result.plan.sources) == 0 {
		t.Fatal("missing prepared source proofs")
	}
	sourcePins, err := decodeSourcePreparationPins(result.plan.sources[0])
	if err != nil {
		t.Fatal(err)
	}
	pixelID := sourcePins[1].Node.Ref.ArtifactID
	var previousExpiry *time.Time
	if err := inspector.QueryRow(ctx, "SELECT expires_at FROM photo.artifacts WHERE tenant_id=$1 AND id=$2", f.Tenant, pixelID).Scan(&previousExpiry); err != nil {
		t.Fatal(err)
	}
	sourceRace := &afterSecondMaskOpen{delegate: objects, key: protectionKey, opens: 1, after: func(ctx context.Context) error {
		_, err := inspector.Exec(ctx, "UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE tenant_id=$1 AND id=$2", f.Tenant, pixelID)
		return err
	}}
	raceReader.Objects = sourceRace
	raceInputs, raceDocuments, raceErr = raceReader.PrepareTransferInputs(ctx, who, request, publishedForRace, preparer.Configuration)
	if !sourceRace.done || raceErr == nil || raceInputs != nil || raceDocuments != nil {
		t.Fatal("source expired during later IO accepted", raceErr, sourceRace.done)
	}
	if _, err := inspector.Exec(ctx, "UPDATE photo.artifacts SET expires_at=$3 WHERE tenant_id=$1 AND id=$2", f.Tenant, pixelID, previousExpiry); err != nil {
		t.Fatal(err)
	}
	t.Log("V27_PREPARER_SOURCE_LATE_IO_RACE_OK")
	// The shared protection root opens once for source protection, once for
	// selection separation, and once for target protection. The third open
	// follows validation of both reference and target RGB.
	finalSourceRace := &afterSecondMaskOpen{delegate: objects, key: protectionKey, at: 3, after: func(ctx context.Context) error {
		_, err := inspector.Exec(ctx, "UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE tenant_id=$1 AND id=$2", f.Tenant, pixelID)
		return err
	}}
	raceReader.Objects = finalSourceRace
	raceInputs, raceDocuments, raceErr = raceReader.PrepareTransferInputs(ctx, who, request, publishedForRace, preparer.Configuration)
	if !finalSourceRace.done || finalSourceRace.opens != 3 || raceErr == nil || raceInputs != nil || raceDocuments != nil {
		t.Fatal("source expired after target RGB validation accepted", raceErr, finalSourceRace.done, finalSourceRace.opens)
	}
	if _, err := inspector.Exec(ctx, "UPDATE photo.artifacts SET expires_at=$3 WHERE tenant_id=$1 AND id=$2", f.Tenant, pixelID, previousExpiry); err != nil {
		t.Fatal(err)
	}
	t.Log("V27_PREPARER_SOURCE_FINAL_IO_RACE_OK")
	sourceTx, err := scheduler.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer sourceTx.Rollback(context.Background())
	if err := LockPreparedSourcesInTransaction(ctx, sourceTx, who, request, result.plan); err != nil {
		t.Fatal("prepared source transaction", err)
	}
	sourceIDs := map[uuid.UUID]bool{}
	for _, proof := range result.plan.sources {
		pins, err := decodeSourcePreparationPins(proof)
		if err != nil {
			t.Fatal(err)
		}
		for _, pin := range pins {
			sourceIDs[pin.Node.Ref.ArtifactID] = true
		}
	}
	writer, err := inspector.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = writer.Exec(ctx, "SET lock_timeout='250ms'"); err != nil {
		t.Fatal(err)
	}
	for id := range sourceIDs {
		_, err = writer.Exec(ctx, "UPDATE photo.artifacts SET expires_at=expires_at WHERE tenant_id=$1 AND id=$2", f.Tenant, id)
		var blocked *pgconn.PgError
		if !errors.As(err, &blocked) || blocked.Code != "55P03" {
			t.Fatal("prepared source artifact crossed lock", id, err)
		}
	}
	writer.Exec(context.Background(), "RESET lock_timeout")
	writer.Release()
	if err := sourceTx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	releaseTx, err := inspector.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseTx.Rollback(context.Background())
	if _, err = releaseTx.Exec(ctx, "SET LOCAL lock_timeout='250ms'"); err != nil {
		t.Fatal(err)
	}
	for id := range sourceIDs {
		tag, err := releaseTx.Exec(ctx, "UPDATE photo.artifacts SET expires_at=expires_at WHERE tenant_id=$1 AND id=$2", f.Tenant, id)
		if err != nil || tag.RowsAffected() != 1 {
			t.Fatal("prepared source lock not released", id, err)
		}
	}
	if err := releaseTx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	t.Log("V27_PREPARER_SOURCE_LOCK_RELEASE_OK")
	if f.FailManifest && result.transports[0].Chunks[0] != retainedChunk {
		t.Fatal("retry replaced completed chunk")
	}
	nextCfg := cfg.Copy()
	next, err := pgxpool.NewWithConfig(ctx, nextCfg)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	preparer.Scheduler = next
	if result.plan.transferQualification == nil || !result.plan.transferQualificationRequired {
		t.Fatal("preparer lost original proof")
	}
	if _, err := inspector.Exec(ctx, "UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=$1", q.PolicyID); err != nil {
		t.Fatal(err)
	}
	revoked, revokedErr := preparer.Prepare(ctx, who, f.Tenant, "proposal-publication", body)
	if revokedErr != v2input.ErrInvalid || revoked.RunID != uuid.Nil || revoked.plan.transferQualification != nil {
		t.Fatal("recovery accepted revoked original qualification", revokedErr)
	}
	if _, err := inspector.Exec(ctx, "UPDATE photo.v2_qualification_policies SET admission_enabled=true WHERE id=$1", q.PolicyID); err != nil {
		t.Fatal(err)
	}
	preparer.TransportDocuments = artifactjson.Publisher{Repository: artifactjson.SQLRepository{DB: next, Bucket: f.Objects.Bucket}, Objects: objects}
	replay, err := preparer.Prepare(ctx, who, f.Tenant, "proposal-publication", body)
	if err != nil || replay.Outcome != "pending" || replay.RunID != result.RunID || replay.transports[0].Manifest != result.transports[0].Manifest {
		t.Fatal("fresh preparer replay", err)
	}
	if replay.plan.transferQualification == nil || !replay.plan.transferQualificationRequired || *replay.plan.transferQualification != *result.plan.transferQualification || replay.plan.transferQualification == result.plan.transferQualification {
		t.Fatal("recovery did not independently rebuild original proof")
	}
	t.Log("V27_PREPARER_QUALIFICATION_RECOVERY_OK")
	t.Log("V27_PREPARER_FRESH_REPLAY_OK")
	t.Log("V27_PROPOSAL_PUBLICATION_OK")
	t.Log("V27_TRANSFER_BATCH_PUBLICATION_OK")
}
