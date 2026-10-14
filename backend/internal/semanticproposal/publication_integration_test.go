package semanticproposal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"os"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/masknpy"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
	"strings"
	"testing"
	"time"
)

func TestIntegrationProposalIntentPublication(t *testing.T) {
	raw := os.Getenv("PHOTO_SEMANTIC_INTENT_PUBLISH_CASE")
	if raw == "" {
		t.Skip("isolated database/object fixture required")
	}
	var fixture struct {
		Tenant, Actor, Session    uuid.UUID
		Intent                    v2input.SemanticEditIntent
		Objects                   objectstore.Config
		Protection                v2input.Ref
		RequiredProtection        v2input.Ref
		DefectPins                DefectOriginPins
		IncompletePNGDefectPins   DefectOriginPins
		FaceDefectPins            DefectOriginPins
		MultiFaceDefectPins       DefectOriginPins
		UncoveredManualDefectPins DefectOriginPins
		ManualFaceDefectTask      uuid.UUID
		ManualOutputs             [3]v2input.Ref
		ManualFaceDefectPins      DefectOriginPins
		SkinFaceDefectPins        DefectOriginPins
		SkinMaskRoot              v2input.Ref
		SkinLabels                v2input.Ref
		SkinPNG                   v2input.Ref
		SkinParse                 v2input.Ref
		SecondManualTarget        *review.ProtectionMask
		CommitOrdinary            bool
		CreateOrdinary            bool
		CreatePerson              bool
		ManualRaceOnly            bool
		ManualLockOnly            bool
		OrientedSource            bool
		ManualVectors             *v2input.Ref
		ManualFaceMaskRoot        v2input.Ref
		SecondDefectLandmark      v2input.Ref
		SecondDefectFaceID        uuid.UUID
		DefectLandmark            v2input.Ref
		DefectDetection           v2input.Ref
		DefectFaceID              uuid.UUID
		PersonReport              v2input.Ref
		PersonMask                v2input.Ref
		AmbiguousPersonMask       v2input.Ref
		MaskBody                  json.RawMessage
		SourceBody                json.RawMessage
		MeasuredTransferCosts     bool
		Qualification             v2input.SemanticQualificationPin
		PersonQualification       v2input.SemanticQualificationPin
		DefectQualification       v2input.SemanticQualificationPin
		FaceQualifications        [2]v2input.SemanticQualificationPin
		ParseQualification        v2input.SemanticQualificationPin
		TransferBatch             bool
		Revoked                   bool
		RevokeDuringPut           bool
		SourceDuringPut           bool
	}
	if json.Unmarshal([]byte(raw), &fixture) != nil {
		t.Fatal("invalid fixture")
	}
	testDeadline := 180 * time.Second
	if fixture.ManualLockOnly {
		testDeadline = 480 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), testDeadline)
	defer cancel()
	started := time.Now()
	queryTiming := &integrationQueryTiming{}
	defer func() {
		t.Logf("V27_TEST_DURATION_SECONDS %.3f", time.Since(started).Seconds())
		queryTiming.log(t)
	}()
	cfg, err := pgxpool.ParseConfig(os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal("database configuration")
	}
	cfg.ConnConfig.RuntimeParams["role"] = "photo_api"
	cfg.ConnConfig.Tracer = queryTiming
	api, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal("database unavailable")
	}
	defer api.Close()
	objects, err := objectstore.New(fixture.Objects)
	if err != nil {
		t.Fatal("object configuration")
	}
	publisher := v2draft.Publisher{DB: api, Objects: objects, Bucket: fixture.Objects.Bucket}
	who := auth.Identity{UserID: fixture.Actor, SessionID: fixture.Session}
	body, _ := canonical.Marshal(fixture.Intent)
	request, err := CompileRequest(fixture.Tenant, fixture.Actor, "proposal-publication", body)
	if err != nil {
		t.Fatal(err)
	}
	if fixture.Revoked {
		if _, err := PublishIntent(ctx, publisher, who, request); err == nil {
			t.Fatal("revoked session replayed proposal")
		}
		t.Log("V27_PROPOSAL_REVOKED_OK")
		return
	}
	if fixture.RevokeDuringPut || fixture.SourceDuringPut {
		owner, err := pgxpool.New(ctx, os.Getenv("PHOTO_TEST_DATABASE_URL"))
		if err != nil {
			t.Fatal("fixture administration")
		}
		defer owner.Close()
		called := false
		publisher.Objects = afterIntentPut{ImmutableStore: objects, after: func() error {
			called = true
			var err error
			if fixture.SourceDuringPut {
				_, err = owner.Exec(ctx, "UPDATE photo.asset_versions SET review_state='proposed' WHERE id=$1", fixture.Intent.Reference.AssetVersionID)
			} else {
				_, err = owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1", fixture.Session)
			}
			return err
		}}
		if _, err := PublishIntent(ctx, publisher, who, request); err == nil || !called {
			t.Fatal("revocation during PUT accepted", err)
		}
		if fixture.SourceDuringPut {
			t.Log("V27_PROPOSAL_SOURCE_DURING_PUT_OK")
		} else {
			t.Log("V27_PROPOSAL_REVOKE_DURING_PUT_OK")
		}
		return
	}
	first, err := PublishIntent(ctx, publisher, who, request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := PublishIntent(ctx, publisher, who, request)
	if err != nil || first.Ref != second.Ref || first.Inventory.Scope != second.Inventory.Scope || len(second.Inventory.Targets) != 1 {
		t.Fatal("proposal replay", err)
	}

	var preparation TransferPreparation
	var transferRefs []v2input.Ref
	var snapshotBody []byte
	var snapshotRef v2input.Ref
	var personPreparedInputs []TransferTaskInputs
	var personPreparedDocuments []v2input.SemanticPinnedDocuments
	var preparedInputs []TransferTaskInputs
	var preparedDocuments []v2input.SemanticPinnedDocuments
	if fixture.TransferBatch {
		reader := SQLTransferSourceReader{DB: api, Objects: objects, Bucket: fixture.Objects.Bucket, ProjectID: first.Inventory.Scope.ProjectID, ManualRasterLimits: ManualRasterVerificationLimits{PointChecks: 10000000, EdgeEvaluations: 100000000, PayloadBytes: 2000000}, ManualRasterBatchLimits: ManualRasterVerificationLimits{PointChecks: 100000000, EdgeEvaluations: 1000000000, PayloadBytes: 20000000}}
		snapshotRef, snapshotBody, err = reader.PublishTargetSnapshot(ctx, publisher, who, request, "proposal-publication", first)
		if err != nil {
			t.Fatal("snapshot publication", err)
		}
		snapshotReplay, replayBody, err := reader.PublishTargetSnapshot(ctx, publisher, who, request, "proposal-publication", first)
		if err != nil || snapshotReplay != snapshotRef || !bytes.Equal(replayBody, snapshotBody) {
			t.Fatal("snapshot publication replay", err)
		}
		t.Log("V27_PROPOSAL_SNAPSHOT_PUBLICATION_OK")
		raw, err := reader.Read(ctx, who, fixture.Tenant, request.Intent.Reference)
		expected, canonicalErr := canonical.Bytes(fixture.SourceBody)
		if err != nil || canonicalErr != nil || !bytes.Equal(raw, expected) {
			t.Fatal("scoped source read", err)
		}
		if _, err := reader.Read(ctx, who, uuid.New(), request.Intent.Reference); err == nil {
			t.Fatal("foreign tenant source")
		}
		wrongReader := reader
		wrongReader.ProjectID = uuid.New()
		if _, err := wrongReader.Read(ctx, who, fixture.Tenant, request.Intent.Reference); err == nil {
			t.Fatal("foreign project source")
		}
		badBinding := request.Intent.Reference
		badBinding.CanvasHash = strings.Repeat("0", 64)
		if _, err := reader.Read(ctx, who, fixture.Tenant, badBinding); err == nil {
			t.Fatal("wrong canvas source")
		}
		t.Log("V27_PROPOSAL_SOURCE_READ_OK")
		metadata, err := reader.VerifySourcePixels(ctx, who, fixture.Tenant, request.Intent.Reference)
		if err != nil || metadata.Width != 400 || metadata.Height != 300 || metadata.Inputs.Alpha == nil {
			t.Fatal("scoped RGB verification", err)
		}
		t.Log("V27_PROPOSAL_SOURCE_PIXELS_OK")
		faceSource, faceSourceErr := reader.VerifyFaceSourcePixels(ctx, who, fixture.Tenant, request.Intent.Reference)
		if fixture.OrientedSource || fixture.CreatePerson {
			if faceSourceErr != nil || faceSource.Width != 400 {
				t.Fatal("unit RGB face source", faceSourceErr)
			}
		} else if faceSourceErr == nil || faceSource.Width != 0 {
			t.Fatal("HDR outside face worker input contract accepted")
		}
		color, err := LoadTransferColorProfile("../../../resources/icc")
		if err != nil {
			t.Fatal("runtime ICC registry", err)
		}
		qualified, err := reader.VerifyQualifiedSourcePixels(ctx, who, fixture.Tenant, request.Intent.Reference, color)
		expectedGeometry := 1
		if fixture.OrientedSource {
			expectedGeometry = 0
		}
		if err != nil || qualified.Width != 400 || qualified.Inputs.Alpha == nil || len(qualified.Geometry) != expectedGeometry {
			t.Fatal("qualified source RGB/alpha", err)
		}
		if _, err := reader.VerifyQualifiedSourcePixels(ctx, who, fixture.Tenant, request.Intent.Reference, TransferColorProfile{}); err == nil {
			t.Fatal("missing color qualification accepted")
		}
		t.Log("V27_PROPOSAL_QUALIFIED_COLOR_OK")
		maskBody, err := reader.ReadSelectedMask(ctx, who, request)
		expectedMask, canonicalErr := canonical.Bytes(fixture.MaskBody)
		if err != nil || canonicalErr != nil || !bytes.Equal(maskBody, expectedMask) {
			t.Fatal("scoped selected mask read", err)
		}
		t.Log("V27_PROPOSAL_MASK_READ_OK")
		pixelRef, err := reader.VerifySelectedMaskPixels(ctx, who, request)
		if err != nil || pixelRef.ArtifactID == uuid.Nil {
			t.Fatal("scoped selected pixels", err)
		}
		t.Log("V27_PROPOSAL_MASK_PIXELS_OK")
		protection, err := reader.ReadProtection(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.Protection)
		if err != nil || protection.Root != fixture.Protection || protection.Pixels.ArtifactID == uuid.Nil {
			t.Fatal("canonical protection read", err)
		}
		changedProtection := fixture.Protection
		changedProtection.ArtifactID = uuid.New()
		if _, err := reader.ReadProtection(ctx, who, fixture.Tenant, request.Intent.Reference, changedProtection); err == nil {
			t.Fatal("foreign protection accepted")
		}
		if err := reader.VerifyPersonMaskOrigin(ctx, who, fixture.Tenant, request.Intent.Reference, protection.Root, protection.Pixels); err != v2input.ErrInvalid {
			t.Fatal("manual mask accepted as person origin", err)
		}
		t.Log("V27_PROPOSAL_MANUAL_PERSON_DENIED_OK")
		person, personErr := reader.ReadPersonMask(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.PersonMask, strings.Repeat("c", 64))
		if personErr != nil || person.Root != fixture.PersonMask || person.Pixels.ArtifactID == uuid.Nil {
			t.Fatal("person mask read", personErr)
		}
		denied, personErr := reader.ReadPersonMask(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.PersonMask, strings.Repeat("d", 64))
		if personErr != v2input.ErrInvalid || denied.Root.ArtifactID != uuid.Nil {
			t.Fatal("person model substitution accepted", personErr)
		}
		evidence, evidenceErr := reader.ReadPersonEvidence(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.PersonMask, strings.Repeat("c", 64), "cuda")
		if evidenceErr != nil || evidence.Root != fixture.PersonMask || evidence.Pixels != person.Pixels {
			t.Fatal("person evidence", evidenceErr)
		}
		t.Log("V27_PROPOSAL_PERSON_EVIDENCE_OK")
		personCoverage, personCoverageErr := reader.VerifyPersonProtectionCoverage(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.PersonMask, fixture.RequiredProtection, strings.Repeat("c", 64), "cuda")
		if personCoverageErr != nil || personCoverage != 1 {
			t.Fatal("person protection coverage", personCoverage, personCoverageErr)
		}
		personCoverage, personCoverageErr = reader.VerifyPersonProtectionCoverage(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.PersonMask, fixture.Protection, strings.Repeat("c", 64), "cuda")
		if !errors.Is(personCoverageErr, masknpy.ErrInvalid) || personCoverage != 0 {
			t.Fatal("uncovered person accepted", personCoverage, personCoverageErr)
		}
		t.Log("V27_PROPOSAL_PERSON_COVERAGE_OK")

		evidenceOwner, err := pgxpool.New(ctx, os.Getenv("PHOTO_TEST_DATABASE_URL"))
		if err != nil {
			t.Fatal(err)
		}
		defer evidenceOwner.Close()
		if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1", fixture.PersonReport.ArtifactID); err != nil {
			t.Fatal(err)
		}
		missing, missingErr := reader.ReadPersonEvidence(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.PersonMask, strings.Repeat("c", 64), "cuda")
		if missingErr != v2input.ErrInvalid || missing.Root.ArtifactID != uuid.Nil {
			t.Fatal("person evidence without canonical report accepted", missingErr)
		}
		if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=NULL WHERE id=$1", fixture.PersonReport.ArtifactID); err != nil {
			t.Fatal(err)
		}
		covering, err := reader.ReadProtection(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.RequiredProtection)
		if err != nil {
			t.Fatal(err)
		}
		_, retainedProtectionProof, err := reader.readProtectionPreparationProof(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.RequiredProtection)
		if err != nil {
			t.Fatal("retained protection proof", err)
		}
		if err := reader.recheckProtectionPreparationProof(ctx, who, fixture.Tenant, request.Intent.Reference, retainedProtectionProof); err != nil {
			t.Fatal("original protection proof recheck", err)
		}
		if err := reader.recheckProtectionPreparationProof(ctx, who, uuid.New(), request.Intent.Reference, retainedProtectionProof); !errors.Is(err, v2input.ErrInvalid) {
			t.Fatal("foreign protection proof", err)
		}
		if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1", covering.Pixels.ArtifactID); err != nil {
			t.Fatal(err)
		}
		if err := reader.recheckProtectionPreparationProof(ctx, who, fixture.Tenant, request.Intent.Reference, retainedProtectionProof); err == nil {
			t.Fatal("expired protection pixels accepted")
		}
		if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=NULL WHERE id=$1", covering.Pixels.ArtifactID); err != nil {
			t.Fatal(err)
		}
		if err := reader.recheckProtectionPreparationProof(ctx, who, fixture.Tenant, request.Intent.Reference, retainedProtectionProof); err != nil {
			t.Fatal("restored protection proof", err)
		}
		t.Log("V27_PROTECTION_PREPARATION_RETAINED_PROOF_OK")
		personPolicyPin := fixture.PersonQualification
		coverageExpected := v2qualification.Expected{Operation: "mask.person", Version: "1", Backend: "cuda", ModelLockSHA: personPolicyPin.ModelLockSHA, RuntimeLockSHA: personPolicyPin.RuntimeLockSHA, DeviceProfileSHA: personPolicyPin.DeviceProfileSHA, DatasetSHA: personPolicyPin.DatasetSHA, IssuerID: personPolicyPin.IssuerID, RequiredChecks: []string{"quality"}, TestFixtureSHAs: map[string]struct{}{strings.Repeat("e", 64): {}}, EvidenceSHAs: map[string]struct{}{strings.Repeat("f", 64): {}}, CalibrationSHAs: map[string]struct{}{personPolicyPin.Calibration.SHA256: {}}, LicenseCleared: true}
		retainedCoverageProof, err := reader.selectPersonProtectionProof(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.RequiredProtection, personPolicyPin.Report, coverageExpected)
		if err != nil || retainedCoverageProof.Root != fixture.PersonMask || retainedCoverageProof.Count != 1 || retainedCoverageProof.protectionEvidence != retainedProtectionProof {
			t.Fatal("qualified coverage lost original protection pins", err)
		}
		t.Log("V27_PERSON_COVERAGE_PROTECTION_PROOF_OK")
		coveringCatalog := v2draft.SQLArtifactCatalog{DB: api, Bucket: fixture.Objects.Bucket, ProjectID: first.Inventory.Scope.ProjectID, AssetVersionID: request.Intent.Reference.AssetVersionID}
		coveringObject, err := coveringCatalog.Lookup(ctx, who, fixture.Tenant, v2draft.LayerRef{Ref: covering.Pixels, Contract: "MaskPixels", ParentRef: covering.Root})
		if err != nil {
			t.Fatal(err)
		}
		raceObjects := &afterSecondMaskOpen{delegate: objects, key: coveringObject.Key, after: func(ctx context.Context) error {
			_, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1", fixture.PersonReport.ArtifactID)
			return err
		}}
		raceReader := reader
		raceReader.Objects = raceObjects
		racedCount, racedErr := raceReader.VerifyPersonProtectionCoverage(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.PersonMask, fixture.RequiredProtection, strings.Repeat("c", 64), "cuda")
		if racedErr != v2input.ErrInvalid || racedCount != 0 || !raceObjects.done {
			t.Fatal("expired report during coverage accepted", racedCount, racedErr, raceObjects.done)
		}
		if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=NULL WHERE id=$1", fixture.PersonReport.ArtifactID); err != nil {
			t.Fatal(err)
		}
		t.Log("V27_PROPOSAL_PERSON_COVERAGE_REPORT_RACE_OK")
		t.Log("V27_PROPOSAL_PERSON_REPORT_REQUIRED_OK")
		t.Log("V27_PROPOSAL_PERSON_MASK_READ_OK")
		wrongDefectPins := DefectOriginPins{Proposal: fixture.PersonMask, Report: fixture.PersonReport, Labels: pixelRef, Protection: person.Pixels, Params: fixture.PersonQualification.Report}
		if err := reader.VerifyDefectOrigin(ctx, who, fixture.Tenant, request.Intent.Reference, wrongDefectPins); err != v2input.ErrInvalid {
			t.Fatal("person producer accepted as defects.propose", err)
		}
		t.Log("V27_DEFECT_PERSON_PRODUCER_DENIED_OK")
		if err := reader.VerifyDefectOrigin(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.DefectPins); err != nil {
			t.Fatal("canonical defect origin", err)
		}
		if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 day' WHERE id=$1", fixture.DefectPins.Report.ArtifactID); err != nil {
			t.Fatal(err)
		}
		if err := reader.VerifyDefectOrigin(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.DefectPins); err != v2input.ErrInvalid {
			t.Fatal("expired defect report accepted", err)
		}
		if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=NULL WHERE id=$1", fixture.DefectPins.Report.ArtifactID); err != nil {
			t.Fatal(err)
		}
		if err := reader.VerifyDefectOrigin(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.DefectPins); err != nil {
			t.Fatal("restored defect origin", err)
		}
		t.Log("V27_DEFECT_CANONICAL_ORIGIN_OK")
		defectEvidence, err := reader.ReadDefectEvidence(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.DefectPins)
		if err != nil || defectEvidence.Labels != fixture.DefectPins.Labels || defectEvidence.Protection != fixture.DefectPins.Protection || len(defectEvidence.Params) == 0 || len(defectEvidence.Report) == 0 || len(defectEvidence.Proposal) == 0 {
			t.Fatal("original defect evidence", err)
		}
		if defectEvidence.LabelPNG.ArtifactID == uuid.Nil {
			t.Fatal("missing verified label PNG")
		}
		pngObject, err := reader.defectLabelPNGObject(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.DefectPins, defectEvidence.LabelPNG)
		if err != nil {
			t.Fatal("canonical label PNG", err)
		}
		assertNoPNGPartial := func(e VerifiedDefectEvidence, err error) {
			t.Helper()
			if err == nil || len(e.Params) != 0 || len(e.Proposal) != 0 || len(e.Report) != 0 || e.Labels.ArtifactID != uuid.Nil || e.Protection.ArtifactID != uuid.Nil || e.LabelPNG.ArtifactID != uuid.Nil || len(e.FaceMasks) != 0 {
				t.Fatal("invalid PNG returned partial defect evidence", err)
			}
		}
		var declarationProducer uuid.UUID
		var declarationInputs json.RawMessage
		if err := evidenceOwner.QueryRow(ctx, "SELECT producer.id,producer.inputs FROM photo.artifacts report JOIN photo.tasks producer ON producer.tenant_id=report.tenant_id AND producer.id=report.created_by_task_id WHERE report.id=$1", fixture.DefectPins.Report.ArtifactID).Scan(&declarationProducer, &declarationInputs); err != nil {
			t.Fatal(err)
		}
		if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.tasks SET inputs=jsonb_build_array(jsonb_build_object('artifact_id',$2::uuid,'sha256',$3::text)) WHERE id=$1", declarationProducer, request.Intent.Reference.Source.ArtifactID, request.Intent.Reference.Source.SHA256); err != nil {
			t.Fatal(err)
		}
		if err := reader.VerifyDefectOrigin(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.DefectPins); err != nil {
			t.Fatal("source-only origin fixture", err)
		}
		undeclaredProtection, undeclaredProtectionErr := reader.ReadDefectEvidence(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.DefectPins)
		assertNoPNGPartial(undeclaredProtection, undeclaredProtectionErr)
		if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.tasks SET inputs=$2::jsonb WHERE id=$1", declarationProducer, declarationInputs); err != nil {
			t.Fatal(err)
		}
		if restored, err := reader.ReadDefectEvidence(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.DefectPins); err != nil || restored.Protection != defectEvidence.Protection {
			t.Fatal("restored declared protection", err)
		}
		if err := reader.VerifyDefectOrigin(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.IncompletePNGDefectPins); err != nil {
			t.Fatal("incomplete PNG fixture lost four-output origin", err)
		}
		incompletePNG, incompletePNGErr := reader.ReadDefectEvidence(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.IncompletePNGDefectPins)
		assertNoPNGPartial(incompletePNG, incompletePNGErr)
		corruptPNG := &corruptDefectOpen{delegate: objects, key: pngObject.object.Key}
		corruptPNGReader := reader
		corruptPNGReader.Objects = corruptPNG
		deniedPNG, deniedPNGErr := corruptPNGReader.ReadDefectEvidence(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.DefectPins)
		assertNoPNGPartial(deniedPNG, deniedPNGErr)
		if !corruptPNG.done {
			t.Fatal("PNG corruption hook did not run")
		}
		for _, expiredRef := range []v2input.Ref{defectEvidence.LabelPNG, fixture.DefectPins.Report} {
			racePNG := &afterSecondMaskOpen{delegate: objects, key: pngObject.object.Key, opens: 1, after: func(ctx context.Context) error {
				_, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 day' WHERE id=$1", expiredRef.ArtifactID)
				return err
			}}
			racePNGReader := reader
			racePNGReader.Objects = racePNG
			denied, err := racePNGReader.ReadDefectEvidence(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.DefectPins)
			assertNoPNGPartial(denied, err)
			if !racePNG.done {
				t.Fatal("PNG expiry hook did not run")
			}
			if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=NULL WHERE id=$1", expiredRef.ArtifactID); err != nil {
				t.Fatal(err)
			}
		}
		var pngResultID uuid.UUID
		if err := evidenceOwner.QueryRow(ctx, "SELECT result.id FROM photo.artifacts output JOIN photo.tasks producer ON producer.id=output.created_by_task_id AND producer.tenant_id=output.tenant_id JOIN photo.task_results result ON result.id=producer.canonical_result_id AND result.tenant_id=producer.tenant_id WHERE output.id=$1", defectEvidence.LabelPNG.ArtifactID).Scan(&pngResultID); err != nil {
			t.Fatal(err)
		}
		if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.task_results SET outputs=(SELECT coalesce(jsonb_agg(entry),'[]'::jsonb) FROM jsonb_array_elements(outputs) entry WHERE entry->>'artifact_id'<>$2) WHERE id=$1", pngResultID, defectEvidence.LabelPNG.ArtifactID.String()); err == nil {
			t.Fatal("canonical PNG output inventory mutation accepted")
		} else {
			var databaseError *pgconn.PgError
			if !errors.As(err, &databaseError) || databaseError.Code != "23514" {
				t.Fatal("unexpected output inventory refusal", err)
			}
		}
		if foreign, err := reader.defectLabelPNGObject(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.DefectPins, fixture.PersonMask); err == nil || foreign.ref.ArtifactID != uuid.Nil {
			t.Fatal("foreign producer artifact accepted as defect PNG", err)
		}
		if restored, err := reader.ReadDefectEvidence(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.DefectPins); err != nil || restored.LabelPNG != defectEvidence.LabelPNG {
			t.Fatal("restored PNG evidence", err)
		}
		t.Log("V27_DEFECT_ORIGINAL_EVIDENCE_OK")
		defectFaces, faceErr := reader.ReadDefectFaces(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.DefectPins)
		if faceErr != nil || len(defectFaces) != 0 {
			t.Fatal("valid no-face defect evidence", defectFaces, faceErr)
		}
		if _, err := reader.defectLandmarkObject(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.DefectPins, fixture.PersonReport); err != v2input.ErrInvalid {
			t.Fatal("person report accepted as landmark", err)
		}
		t.Log("V27_DEFECT_LANDMARK_PERSON_DENIED_OK")
		t.Log("V27_DEFECT_EMPTY_FACES_READ_OK")
		populatedFaces, faceErr := reader.ReadDefectFaces(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.FaceDefectPins)
		if faceErr != nil || len(populatedFaces) != 1 || populatedFaces[0].ObjectID != fixture.DefectFaceID || len(populatedFaces[0].Points) != 68 {
			t.Fatal("canonical nonempty defect faces", populatedFaces, faceErr)
		}
		if _, err := reader.defectLandmarkObject(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.DefectPins, fixture.DefectLandmark); err != v2input.ErrInvalid {
			t.Fatal("undeclared landmark accepted", err)
		}
		for _, producerOutput := range []v2input.Ref{fixture.FaceDefectPins.Proposal, fixture.DefectLandmark, fixture.DefectDetection} {
			var producerID uuid.UUID
			var finished time.Time
			if err := evidenceOwner.QueryRow(ctx, "SELECT a.created_by_task_id,attempt.finished_at FROM photo.artifacts a JOIN photo.tasks t ON t.tenant_id=a.tenant_id AND t.id=a.created_by_task_id JOIN photo.task_attempts attempt ON attempt.tenant_id=t.tenant_id AND attempt.task_id=t.id AND attempt.attempt=t.attempt AND attempt.fencing_token=t.fencing_token WHERE a.id=$1", producerOutput.ArtifactID).Scan(&producerID, &finished); err != nil {
				t.Fatal(err)
			}
			if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.task_attempts SET finished_at=NULL WHERE task_id=$1", producerID); err != nil {
				t.Fatal(err)
			}
			rejected, attemptErr := reader.ReadDefectFaces(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.FaceDefectPins)
			if attemptErr != v2input.ErrInvalid || rejected != nil {
				t.Fatal("unfinished canonical attempt accepted", attemptErr)
			}
			if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.task_attempts SET finished_at=$2 WHERE task_id=$1", producerID, finished); err != nil {
				t.Fatal(err)
			}
			if _, err := evidenceOwner.Exec(ctx, `UPDATE photo.task_attempts SET profile=jsonb_set(profile,'{backend}','"cuda"'::jsonb) WHERE task_id=$1`, producerID); err != nil {
				t.Fatal(err)
			}
			rejected, attemptErr = reader.ReadDefectFaces(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.FaceDefectPins)
			if attemptErr != v2input.ErrInvalid || rejected != nil {
				t.Fatal("inconsistent attempt backend accepted", attemptErr)
			}
			if _, err := evidenceOwner.Exec(ctx, `UPDATE photo.task_attempts SET profile=jsonb_set(profile,'{backend}','"cpu"'::jsonb) WHERE task_id=$1`, producerID); err != nil {
				t.Fatal(err)
			}
		}
		restoredFaces, restoredErr := reader.ReadDefectFaces(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.FaceDefectPins)
		if restoredErr != nil || len(restoredFaces) != 1 {
			t.Fatal("restored producer attempts", restoredErr)
		}
		t.Log("V27_DEFECT_FINISHED_ATTEMPT_BACKEND_REQUIRED_OK")
		faceProfiles := [2]v2qualification.Expected{
			{Operation: "face.landmarks", Version: "1", Backend: "cpu", ModelLockSHA: strings.Repeat("c", 64), RuntimeLockSHA: strings.Repeat("b", 64), DeviceProfileSHA: strings.Repeat("c", 64)},
			{Operation: "face.detect", Version: "1", Backend: "cpu", ModelLockSHA: strings.Repeat("c", 64), RuntimeLockSHA: strings.Repeat("b", 64), DeviceProfileSHA: strings.Repeat("c", 64)},
		}
		if err := reader.VerifyDefectFaceProducerProfiles(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.FaceDefectPins, fixture.DefectLandmark, fixture.DefectDetection, faceProfiles); err != nil {
			t.Fatal("face profiles", err)
		}
		wrongProfiles := faceProfiles
		wrongProfiles[0].RuntimeLockSHA = strings.Repeat("d", 64)
		if err := reader.VerifyDefectFaceProducerProfiles(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.FaceDefectPins, fixture.DefectLandmark, fixture.DefectDetection, wrongProfiles); err != v2qualification.ErrUnqualified {
			t.Fatal("wrong trusted runtime accepted", err)
		}
		for _, output := range []v2input.Ref{fixture.DefectLandmark, fixture.DefectDetection} {
			var taskID uuid.UUID
			if err := evidenceOwner.QueryRow(ctx, "SELECT created_by_task_id FROM photo.artifacts WHERE id=$1", output.ArtifactID).Scan(&taskID); err != nil {
				t.Fatal(err)
			}
			for field, original := range map[string]string{"model_lock_sha": strings.Repeat("c", 64), "runtime_lock_sha": strings.Repeat("b", 64), "device_profile_sha": strings.Repeat("c", 64)} {
				if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.task_attempts SET profile=jsonb_set(profile,ARRAY[$2]::text[],to_jsonb($3::text)) WHERE task_id=$1", taskID, field, strings.Repeat("d", 64)); err != nil {
					t.Fatal(err)
				}
				if err := reader.VerifyDefectFaceProducerProfiles(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.FaceDefectPins, fixture.DefectLandmark, fixture.DefectDetection, faceProfiles); err != v2qualification.ErrUnqualified {
					t.Fatal("changed attempt lock accepted", field, err)
				}
				if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.task_attempts SET profile=jsonb_set(profile,ARRAY[$2]::text[],to_jsonb($3::text)) WHERE task_id=$1", taskID, field, original); err != nil {
					t.Fatal(err)
				}
			}
		}
		if err := reader.VerifyDefectFaceProducerProfiles(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.FaceDefectPins, fixture.DefectLandmark, fixture.DefectDetection, faceProfiles); err != nil {
			t.Fatal("restored face profiles", err)
		}
		t.Log("V27_DEFECT_FACE_PROFILES_MATCH_REQUIRED_OK")
		qualifiedProfiles := faceProfiles
		var faceQualificationReports [2]v2input.Ref
		for i, q := range fixture.FaceQualifications {
			qualifiedProfiles[i].DatasetSHA = q.DatasetSHA
			qualifiedProfiles[i].IssuerID = q.IssuerID
			qualifiedProfiles[i].RequiredChecks = []string{"quality"}
			qualifiedProfiles[i].TestFixtureSHAs = map[string]struct{}{strings.Repeat("e", 64): {}}
			qualifiedProfiles[i].EvidenceSHAs = map[string]struct{}{strings.Repeat("f", 64): {}}
			qualifiedProfiles[i].CalibrationSHAs = map[string]struct{}{q.Calibration.SHA256: {}}
			qualifiedProfiles[i].LicenseCleared = true
			faceQualificationReports[i] = q.Report
		}
		for i := range qualifiedProfiles {
			if _, err := reader.VerifyFaceQualification(ctx, who, fixture.Tenant, faceQualificationReports[i], qualifiedProfiles[i]); err != nil {
				t.Fatal("original trusted face report reader", err)
			}
		}
		parseExpected := qualifiedProfiles[0]
		parseExpected.Operation = "face.parse"
		parseExpected.DatasetSHA = fixture.ParseQualification.DatasetSHA
		parseExpected.IssuerID = fixture.ParseQualification.IssuerID
		parseExpected.CalibrationSHAs = map[string]struct{}{fixture.ParseQualification.Calibration.SHA256: {}}
		parseReport := fixture.ParseQualification.Report
		if report, err := reader.VerifyFaceQualification(ctx, who, fixture.Tenant, parseReport, parseExpected); err != nil || report.Operation != "face.parse" {
			t.Fatal("trusted original parse qualification", err)
		}
		wrongParse := parseExpected
		wrongParse.RuntimeLockSHA = strings.Repeat("d", 64)
		if _, err := reader.VerifyFaceQualification(ctx, who, fixture.Tenant, parseReport, wrongParse); err != v2qualification.ErrUnqualified {
			t.Fatal("wrong parse runtime accepted", err)
		}
		parseQualificationObject, err := reader.faceQualificationObject(ctx, who, fixture.Tenant, parseExpected, parseReport)
		if err != nil {
			t.Fatal(err)
		}
		corruptParseReport := &corruptDefectOpen{delegate: objects, key: parseQualificationObject.object.Key}
		corruptParseReader := reader
		corruptParseReader.Objects = corruptParseReport
		if _, err := corruptParseReader.VerifyFaceQualification(ctx, who, fixture.Tenant, parseReport, parseExpected); err == nil || !corruptParseReport.done {
			t.Fatal("corrupt original parse qualification accepted", err)
		}

		if _, err := reader.VerifyFaceQualification(ctx, who, fixture.Tenant, faceQualificationReports[0], parseExpected); err != v2qualification.ErrUnqualified {
			t.Fatal("landmark report accepted for face.parse", err)
		}
		qualifiedFaces, qualifiedFaceErr := reader.ReadQualifiedDefectFaces(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.FaceDefectPins, fixture.DefectLandmark, fixture.DefectDetection, faceQualificationReports, qualifiedProfiles)
		if qualifiedFaceErr != nil || len(qualifiedFaces) != 1 || qualifiedFaces[0].ObjectID != fixture.DefectFaceID {
			t.Fatal("trusted qualified face chain", qualifiedFaces, qualifiedFaceErr)
		}
		wrongReports := faceQualificationReports
		wrongReports[0] = fixture.PersonQualification.Report
		rejectedQualified, qualifiedFaceErr := reader.ReadQualifiedDefectFaces(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.FaceDefectPins, fixture.DefectLandmark, fixture.DefectDetection, wrongReports, qualifiedProfiles)
		if qualifiedFaceErr != v2qualification.ErrUnqualified || rejectedQualified != nil {
			t.Fatal("person qualification accepted for landmark", qualifiedFaceErr)
		}
		t.Log("V27_DEFECT_TRUSTED_FACE_QUALIFICATION_READ_OK")
		multiEvidence, maskErr := reader.ReadDefectEvidence(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.MultiFaceDefectPins)
		if maskErr != nil || len(multiEvidence.FaceMasks) != 2 || multiEvidence.FaceMasks[0].Ordinal != 0 || multiEvidence.FaceMasks[1].Ordinal != 1 || multiEvidence.FaceMasks[0].Mask.Selector != "manual" || multiEvidence.FaceMasks[1].Mask.Selector != "manual" || multiEvidence.FaceMasks[0].ObjectID == nil || multiEvidence.FaceMasks[1].ObjectID == nil || *multiEvidence.FaceMasks[0].ObjectID != fixture.DefectFaceID || *multiEvidence.FaceMasks[1].ObjectID != fixture.SecondDefectFaceID {
			t.Fatal("complete original face mask inventory", maskErr)
		}
		manualPixel := multiEvidence.FaceMasks[0].Mask.Artifact
		maskObjects, maskOriginErr := reader.defectFaceMaskObjects(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.MultiFaceDefectPins, v2input.Ref{ArtifactID: manualPixel.ID, SHA256: manualPixel.SHA256}, "manual")
		if maskOriginErr != v2input.ErrInvalid || maskObjects != (defectMaskObjects{}) {
			t.Fatal("synthetic manual pixels without rasterized root accepted", maskOriginErr)
		}
		missingRoots, missingRootsErr := reader.ReadDefectFaceMaskRoots(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.MultiFaceDefectPins)
		if missingRootsErr != v2input.ErrInvalid || missingRoots != nil {
			t.Fatal("incomplete mask inventory accepted", missingRootsErr)
		}
		emptyRoots, emptyRootsErr := reader.ReadDefectFaceMaskRoots(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.DefectPins)
		if emptyRootsErr != nil || emptyRoots == nil || len(emptyRoots) != 0 {
			t.Fatal("valid empty declared face inventory", emptyRootsErr)
		}
		manualRoots, manualRootsErr := reader.ReadDefectFaceMaskRoots(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.ManualFaceDefectPins)
		if !fixture.OrientedSource {
			if manualRootsErr == nil || manualRoots != nil {
				t.Fatal("transformed source rasterization accepted")
			}
		} else {
			if manualRootsErr != nil || len(manualRoots) != 1 || manualRoots[0].Root.ID != fixture.ManualFaceMaskRoot.ArtifactID || manualRoots[0].Root.SHA256 != fixture.ManualFaceMaskRoot.SHA256 || manualRoots[0].Binding.ObjectID == nil || *manualRoots[0].Binding.ObjectID != fixture.DefectFaceID {
				t.Fatal("canonical manual root and NPY read", manualRootsErr)
			}
			if err := reader.VerifyDefectOrigin(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.UncoveredManualDefectPins); err != nil {
				t.Fatal("uncovered manual origin", err)
			}
			if denied, err := reader.ReadDefectEvidence(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.UncoveredManualDefectPins); err == nil || len(denied.Params) != 0 || denied.Labels.ArtifactID != uuid.Nil {
				t.Fatal("uncovered declared protection accepted", err)
			}
			if denied, err := reader.ReadDefectFaceMaskRoots(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.UncoveredManualDefectPins); err == nil || denied != nil {
				t.Fatal("missing original manual protection accepted", err)
			}
			actualManualPixel := manualRoots[0].Binding.Mask.Artifact
			actualManualObjects, actualManualErr := reader.defectFaceMaskObjects(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.ManualFaceDefectPins, v2input.Ref{ArtifactID: actualManualPixel.ID, SHA256: actualManualPixel.SHA256}, "manual")
			if actualManualErr != nil {
				t.Fatal(actualManualErr)
			}
			preProducerObjects, err := reader.manualPreparationObjects(ctx, who, fixture.Tenant, request.Intent.Reference, v2input.Ref{ArtifactID: actualManualPixel.ID, SHA256: actualManualPixel.SHA256})
			if err != nil || preProducerObjects != actualManualObjects {
				t.Fatal("pre-producer original manual catalog mismatch", err)
			}
			if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1", fixture.ManualFaceDefectPins.Report.ArtifactID); err != nil {
				t.Fatal(err)
			}
			independentObjects, independentErr := reader.manualPreparationObjects(ctx, who, fixture.Tenant, request.Intent.Reference, v2input.Ref{ArtifactID: actualManualPixel.ID, SHA256: actualManualPixel.SHA256})
			if independentErr != nil || independentObjects != preProducerObjects {
				t.Fatal("manual preparation depends on defect report", independentErr)
			}
			for i, ref := range []v2input.Ref{fixture.DefectLandmark, fixture.DefectDetection} {
				operation := []string{"face.landmarks", "face.detect"}[i]
				facePins, err := reader.facePreparationObjects(ctx, who, fixture.Tenant, request.Intent.Reference, ref, operation)
				if err != nil || facePins[0].ref != ref || facePins[1].ref.ArtifactID == uuid.Nil {
					t.Fatal("independent original face catalog", err)
				}
				for _, entry := range facePins {
					if _, err := v2input.ReadPinnedSemanticDocument(ctx, objects, entry.object, entry.ref); err != nil {
						t.Fatal("independent face original object IO", err)
					}
				}
			}
			preparationFaces, err := reader.ReadFacePreparation(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.DefectLandmark)
			if err != nil || len(preparationFaces) != 1 || preparationFaces[0].ObjectID != fixture.DefectFaceID {
				t.Fatal("independent original face preparation", err)
			}
			landmarkPins, err := reader.facePreparationObjects(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.DefectLandmark, "face.landmarks")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1", landmarkPins[1].ref.ArtifactID); err != nil {
				t.Fatal(err)
			}
			if denied, err := reader.ReadFacePreparation(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.DefectLandmark); err == nil || denied != nil {
				t.Fatal("expired face Params accepted", err)
			}
			if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=NULL WHERE id=$1", landmarkPins[1].ref.ArtifactID); err != nil {
				t.Fatal(err)
			}
			var preparationProducer uuid.UUID
			var preparationInputs json.RawMessage
			if err := evidenceOwner.QueryRow(ctx, "SELECT producer.id,producer.inputs FROM photo.artifacts root JOIN photo.tasks producer ON producer.tenant_id=root.tenant_id AND producer.id=root.created_by_task_id WHERE root.id=$1", fixture.DefectLandmark.ArtifactID).Scan(&preparationProducer, &preparationInputs); err != nil {
				t.Fatal(err)
			}
			if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.tasks SET inputs=jsonb_build_array(jsonb_build_object('artifact_id',$2::uuid,'sha256',$3::text)) WHERE id=$1", preparationProducer, request.Intent.Reference.Source.ArtifactID, request.Intent.Reference.Source.SHA256); err != nil {
				t.Fatal(err)
			}
			if denied, err := reader.ReadFacePreparation(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.DefectLandmark); err == nil || denied != nil {
				t.Fatal("undeclared detector preparation accepted", err)
			}
			if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.tasks SET inputs=$2::jsonb WHERE id=$1", preparationProducer, preparationInputs); err != nil {
				t.Fatal(err)
			}
			if restored, err := reader.ReadFacePreparation(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.DefectLandmark); err != nil || len(restored) != 1 {
				t.Fatal("restored independent face preparation", err)
			}
			if err := reader.verifyFacePreparationProfiles(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.DefectLandmark, fixture.DefectDetection, qualifiedProfiles); err != nil {
				t.Fatal("preparation exact profiles", err)
			}
			for i := range qualifiedProfiles {
				if _, err := reader.VerifyFaceQualification(ctx, who, fixture.Tenant, faceQualificationReports[i], qualifiedProfiles[i]); err != nil {
					t.Fatal("preparation trusted report", i, err)
				}
			}
			if _, err := reader.VerifyQualifiedFaceSourcePixels(ctx, who, fixture.Tenant, request.Intent.Reference, color); err != nil {
				t.Fatal("preparation source qualification", err)
			}
			preparedSource, retainedSourceProof, err := reader.readQualifiedSourcePreparationProof(ctx, who, fixture.Tenant, request.Intent.Reference, color, true)
			if err != nil || preparedSource.Width == 0 {
				t.Fatal("retained source closure", err)
			}
			if err := reader.recheckSourcePreparationProof(ctx, who, fixture.Tenant, request.Intent.Reference, color, true, retainedSourceProof); err != nil {
				t.Fatal("source closure recheck", err)
			}
			if err := reader.recheckSourcePreparationProof(ctx, who, uuid.New(), request.Intent.Reference, color, true, retainedSourceProof); !errors.Is(err, v2input.ErrInvalid) {
				t.Fatal("foreign source closure", err)
			}
			sizePins, err := decodeSourcePreparationPins(retainedSourceProof)
			if err != nil {
				t.Fatal(err)
			}
			sizeNodes := make([]v2draft.LayerRef, len(sizePins))
			for i, pin := range sizePins {
				sizeNodes[i] = pin.Node
			}
			sizeCatalog := v2draft.SQLArtifactCatalog{DB: api, Bucket: reader.Bucket, ProjectID: reader.ProjectID, AssetVersionID: request.Intent.Reference.AssetVersionID}
			actualSizes, err := v2draft.ReadInputSizes(ctx, sizeCatalog, who, fixture.Tenant, sizeNodes)
			if err != nil || len(actualSizes) != len(sizePins) {
				t.Fatal("authorized source sizes", err)
			}
			for i, size := range actualSizes {
				if size.Ref != sizePins[i].Node.Ref || size.Bytes != sizePins[i].Object.Bytes {
					t.Fatal("source size mismatch")
				}
			}
			if denied, err := v2draft.ReadInputSizes(ctx, sizeCatalog, who, uuid.New(), sizeNodes); err == nil || denied != nil {
				t.Fatal("foreign tenant source sizes", err)
			}
			if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1", preparedSource.Inputs.Pixels.ArtifactID); err != nil {
				t.Fatal(err)
			}
			if err := reader.recheckSourcePreparationProof(ctx, who, fixture.Tenant, request.Intent.Reference, color, true, retainedSourceProof); err == nil {
				t.Fatal("expired source RGB accepted")
			}
			if denied, err := v2draft.ReadInputSizes(ctx, sizeCatalog, who, fixture.Tenant, sizeNodes); err == nil || denied != nil {
				t.Fatal("expired RGB size accepted", err)
			}
			if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=NULL WHERE id=$1", preparedSource.Inputs.Pixels.ArtifactID); err != nil {
				t.Fatal(err)
			}
			if err := reader.recheckSourcePreparationProof(ctx, who, fixture.Tenant, request.Intent.Reference, color, true, retainedSourceProof); err != nil {
				t.Fatal("restored source closure", err)
			}
			t.Log("V27_SOURCE_PREPARATION_RETAINED_PROOF_OK")
			if restored, err := v2draft.ReadInputSizes(ctx, sizeCatalog, who, fixture.Tenant, sizeNodes); err != nil || len(restored) != len(actualSizes) {
				t.Fatal("restored source sizes", err)
			}
			t.Log("V27_SOURCE_AUTHORIZED_INPUT_SIZES_OK")
			faceCandidates, err := reader.facePreparationCandidates(ctx, who, fixture.Tenant, request.Intent.Reference, qualifiedProfiles)
			if err != nil || len(faceCandidates) != 2 {
				t.Fatal("complete selected face catalog", len(faceCandidates), err)
			}
			if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1", fixture.SecondDefectLandmark.ArtifactID); err != nil {
				t.Fatal(err)
			}
			remainingFaces, err := reader.facePreparationCandidates(ctx, who, fixture.Tenant, request.Intent.Reference, qualifiedProfiles)
			if err != nil || len(remainingFaces) != 1 || remainingFaces[0].landmark != fixture.DefectLandmark || remainingFaces[0].detection != fixture.DefectDetection {
				t.Fatal("expired face catalog entry retained", err)
			}
			if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=NULL WHERE id=$1", fixture.SecondDefectLandmark.ArtifactID); err != nil {
				t.Fatal(err)
			}
			t.Log("V27_FACE_PREPARATION_DISCOVERY_OK")
			preparedFaceInventory, err := reader.prepareQualifiedFaceInventory(ctx, who, fixture.Tenant, request.Intent.Reference, faceQualificationReports, qualifiedProfiles, color)
			if err != nil || len(preparedFaceInventory) != 2 {
				t.Fatal("qualified complete selected face inventory", err)
			}
			for _, collection := range preparedFaceInventory {
				if len(collection.faces) != 1 || collection.proof.objects[0][0].ref != collection.candidate.landmark || collection.proof.objects[1][0].ref != collection.candidate.detection {
					t.Fatal("face inventory proof association")
				}
			}
			wrongInventoryReports := faceQualificationReports
			wrongInventoryReports[0] = faceQualificationReports[1]
			if partial, err := reader.prepareQualifiedFaceInventory(ctx, who, fixture.Tenant, request.Intent.Reference, wrongInventoryReports, qualifiedProfiles, color); err == nil || partial != nil {
				t.Fatal("unqualified face inventory leaked", err)
			}
			t.Log("V27_FACE_PREPARATION_INVENTORY_CONTENT_OK")
			qualifiedPreparedFaces, retainedFaceProof, err := reader.readQualifiedFacePreparationProof(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.DefectLandmark, fixture.DefectDetection, faceQualificationReports, qualifiedProfiles, color)
			if err != nil || len(qualifiedPreparedFaces) != 1 {
				t.Fatal("independent qualified face preparation", err)
			}
			if err := reader.recheckFacePreparationProof(ctx, who, fixture.Tenant, request.Intent.Reference, retainedFaceProof); err != nil {
				t.Fatal("retained face proof", err)
			}
			if err := reader.recheckFacePreparationProof(ctx, who, uuid.New(), request.Intent.Reference, retainedFaceProof); !errors.Is(err, v2input.ErrInvalid) {
				t.Fatal("face proof crossed tenant", err)
			}
			if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1", retainedFaceProof.qualification[0].ref.ArtifactID); err != nil {
				t.Fatal(err)
			}
			if err := reader.recheckFacePreparationProof(ctx, who, fixture.Tenant, request.Intent.Reference, retainedFaceProof); err == nil {
				t.Fatal("expired qualification retained", err)
			}
			if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=NULL WHERE id=$1", retainedFaceProof.qualification[0].ref.ArtifactID); err != nil {
				t.Fatal(err)
			}
			if err := reader.recheckFacePreparationProof(ctx, who, fixture.Tenant, request.Intent.Reference, retainedFaceProof); err != nil {
				t.Fatal("restored retained face proof", err)
			}
			t.Log("V27_FACE_PREPARATION_RETAINED_PROOF_OK")
			wrongPreparationProfiles := qualifiedProfiles
			wrongPreparationProfiles[0].RuntimeLockSHA = strings.Repeat("e", 64)
			if denied, err := reader.ReadQualifiedFacePreparation(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.DefectLandmark, fixture.DefectDetection, faceQualificationReports, wrongPreparationProfiles, color); err == nil || denied != nil {
				t.Fatal("unqualified face runtime prepared", err)
			}
			if denied, err := reader.ReadQualifiedFacePreparation(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.DefectLandmark, fixture.DefectDetection, faceQualificationReports, qualifiedProfiles, TransferColorProfile{}); err == nil || denied != nil {
				t.Fatal("unqualified face source color prepared", err)
			}
			t.Log("V27_FACE_PREPARATION_QUALIFIED_OK")
			t.Log("V27_FACE_PREPARATION_CONTENT_OK")
			parsedLabels, labelCounts, err := reader.ReadFaceParsePreparation(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.SkinParse)
			if err != nil || parsedLabels.Labels.ID != fixture.SkinLabels.ArtifactID || labelCounts[1] == 0 {
				t.Fatal("independent parser original content", err)
			}
			if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1", fixture.SkinLabels.ArtifactID); err != nil {
				t.Fatal(err)
			}
			if labels, counts, err := reader.ReadFaceParsePreparation(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.SkinParse); err == nil || labels.Labels.ID != uuid.Nil || counts != ([19]int64{}) {
				t.Fatal("expired parser labels leaked preparation", err)
			}
			if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=NULL WHERE id=$1", fixture.SkinLabels.ArtifactID); err != nil {
				t.Fatal(err)
			}
			t.Log("V27_FACE_PARSE_PREPARATION_CONTENT_OK")
			parseProfiles := qualifiedProfiles
			parseProfiles[0] = ownQualificationExpected(parseProfiles[0])
			parseProfiles[0].Operation = "face.parse"
			if err := reader.verifyFaceParsePreparationProfiles(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.SkinParse, fixture.DefectDetection, fixture.SkinLabels, parseProfiles); err != nil {
				t.Fatal("independent parser profiles", err)
			}
			wrongParseProfiles := parseProfiles
			wrongParseProfiles[0].RuntimeLockSHA = strings.Repeat("d", 64)
			if err := reader.verifyFaceParsePreparationProfiles(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.SkinParse, fixture.DefectDetection, fixture.SkinLabels, wrongParseProfiles); !errors.Is(err, v2qualification.ErrUnqualified) {
				t.Fatal("wrong parser runtime profile accepted", err)
			}
			t.Log("V27_FACE_PARSE_PREPARATION_PROFILES_OK")
			parseReports := [2]v2input.Ref{parseReport, faceQualificationReports[1]}
			parsePolicies := [2]v2qualification.Expected{parseExpected, qualifiedProfiles[1]}
			parseCandidates, discoveryErr := reader.faceParsePreparationCandidates(ctx, who, fixture.Tenant, request.Intent.Reference, parsePolicies)
			if discoveryErr != nil || len(parseCandidates) != 1 || parseCandidates[0] != (faceParsePreparationCandidate{fixture.SkinParse, fixture.DefectDetection, fixture.SkinLabels}) {
				t.Fatal("independent parser candidate discovery", discoveryErr, parseCandidates)
			}
			t.Log("V27_FACE_PARSE_PREPARATION_DISCOVERY_OK")
			if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1", fixture.SkinLabels.ArtifactID); err != nil {
				t.Fatal(err)
			}
			if expiredCandidates, err := reader.faceParsePreparationCandidates(ctx, who, fixture.Tenant, request.Intent.Reference, parsePolicies); err != nil || len(expiredCandidates) != 0 {
				t.Fatal("expired parser labels remained in discovery", err, expiredCandidates)
			}
			if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=NULL WHERE id=$1", fixture.SkinLabels.ArtifactID); err != nil {
				t.Fatal(err)
			}
			if restoredCandidates, err := reader.faceParsePreparationCandidates(ctx, who, fixture.Tenant, request.Intent.Reference, parsePolicies); err != nil || len(restoredCandidates) != 1 || restoredCandidates[0] != parseCandidates[0] {
				t.Fatal("restored parser discovery changed", err, restoredCandidates)
			}
			t.Log("V27_FACE_PARSE_DISCOVERY_EXPIRY_OK")
			preparedParsers, inventoryErr := reader.prepareQualifiedFaceParseInventory(ctx, who, fixture.Tenant, request.Intent.Reference, parseReports, parsePolicies, color)
			if inventoryErr != nil || len(preparedParsers) != 1 || preparedParsers[0].candidate != parseCandidates[0] || preparedParsers[0].labels.Labels.ID != fixture.SkinLabels.ArtifactID || preparedParsers[0].counts[1] == 0 || preparedParsers[0].proof.pixels.ref != fixture.SkinLabels {
				t.Fatal("qualified parser inventory", inventoryErr)
			}
			foreignParserReports := parseReports
			foreignParserReports[0] = faceQualificationReports[0]
			if partialParsers, inventoryErr := reader.prepareQualifiedFaceParseInventory(ctx, who, fixture.Tenant, request.Intent.Reference, foreignParserReports, parsePolicies, color); inventoryErr == nil || partialParsers != nil {
				t.Fatal("parser inventory returned unqualified partial set", inventoryErr)
			}
			t.Log("V27_FACE_PARSE_PREPARATION_INVENTORY_CONTENT_OK")
			if labels, counts, err := reader.ReadQualifiedFaceParsePreparation(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.SkinParse, fixture.DefectDetection, fixture.SkinLabels, parseReports, parsePolicies, color); err != nil || labels.Labels.ID != fixture.SkinLabels.ArtifactID || counts[1] == 0 {
				t.Fatal("qualified independent parser content", err)
			}
			wrongParseReports := parseReports
			wrongParseReports[0] = faceQualificationReports[0]
			if labels, counts, err := reader.ReadQualifiedFaceParsePreparation(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.SkinParse, fixture.DefectDetection, fixture.SkinLabels, wrongParseReports, parsePolicies, color); err == nil || labels.Labels.ID != uuid.Nil || counts != ([19]int64{}) {
				t.Fatal("landmark report qualified parser", err)
			}
			t.Log("V27_FACE_PARSE_PREPARATION_QUALIFIED_OK")
			_, _, retainedParseProof, err := reader.readQualifiedFaceParsePreparationProof(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.SkinParse, fixture.DefectDetection, fixture.SkinLabels, parseReports, parsePolicies, color)
			if err != nil {
				t.Fatal("retained parser proof", err)
			}
			if err := reader.recheckFaceParsePreparationProof(ctx, who, fixture.Tenant, request.Intent.Reference, retainedParseProof); err != nil {
				t.Fatal("original parser proof recheck", err)
			}
			if err := reader.recheckFaceParsePreparationProof(ctx, who, uuid.New(), request.Intent.Reference, retainedParseProof); !errors.Is(err, v2input.ErrInvalid) {
				t.Fatal("foreign parser proof scope", err)
			}
			if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1", parseReport.ArtifactID); err != nil {
				t.Fatal(err)
			}
			if err := reader.recheckFaceParsePreparationProof(ctx, who, fixture.Tenant, request.Intent.Reference, retainedParseProof); err == nil {
				t.Fatal("expired parser report rechecked")
			}
			if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=NULL WHERE id=$1", parseReport.ArtifactID); err != nil {
				t.Fatal(err)
			}
			if err := reader.recheckFaceParsePreparationProof(ctx, who, fixture.Tenant, request.Intent.Reference, retainedParseProof); err != nil {
				t.Fatal("restored parser proof", err)
			}
			t.Log("V27_FACE_PARSE_PREPARATION_RETAINED_PROOF_OK")
			preparedManual, retainedManualProof, preparationErr := reader.readManualPreparationProof(ctx, who, fixture.Tenant, request.Intent.Reference, manualRoots[0].Binding.Mask)
			if preparationErr != nil || preparedManual.Root.ID != preProducerObjects.Root.ref.ArtifactID || len(preparedManual.Body) == 0 || preparedManual.Raster.ShapeCount() == 0 || preparedManual.Raster.Canvas() != preparedManual.Target.Canvas {
				t.Fatal("independent manual content preparation", preparationErr)
			}

			ownedManual, err := reader.readDefectManualExecutionProofs(ctx, who, fixture.Tenant, request.Intent.Reference, manualRoots)
			if err != nil || len(ownedManual) != 1 {
				t.Fatal("owned original manual closure", err)
			}
			originalManualPins, err := marshalManualEvidencePins(retainedManualProof, reader.Bucket)
			if err != nil {
				t.Fatal(err)
			}
			ownedManualPins, err := marshalManualEvidencePins(ownedManual[0].original(), reader.Bucket)
			if err != nil || string(originalManualPins) != string(ownedManualPins) {
				t.Fatal("manual closure changed during ownership", err)
			}
			t.Log("V27_DEFECT_MANUAL_EXECUTION_ORIGINAL_PROOFS_OK")
			if err := reader.recheckManualPreparationProof(ctx, who, fixture.Tenant, request.Intent.Reference, retainedManualProof); err != nil {
				t.Fatal("retained manual proof recheck", err)
			}
			manualLockConfig := cfg.Copy()
			manualLockConfig.ConnConfig.RuntimeParams["role"] = "photo_scheduler"
			manualLockPool, err := pgxpool.NewWithConfig(ctx, manualLockConfig)
			if err != nil {
				t.Fatal("manual lock scheduler pool", err)
			}
			defer manualLockPool.Close()

			defectQ := fixture.DefectQualification
			defectExpected := v2qualification.Expected{Operation: "defects.propose", Version: "1", Backend: defectQ.Backend, ModelLockSHA: defectQ.ModelLockSHA, RuntimeLockSHA: defectQ.RuntimeLockSHA, DeviceProfileSHA: defectQ.DeviceProfileSHA, DatasetSHA: defectQ.DatasetSHA, IssuerID: defectQ.IssuerID, RequiredChecks: []string{"quality"}, TestFixtureSHAs: map[string]struct{}{strings.Repeat("e", 64): {}}, EvidenceSHAs: map[string]struct{}{strings.Repeat("f", 64): {}}, CalibrationSHAs: map[string]struct{}{defectQ.Calibration.SHA256: {}}, LicenseCleared: true}
			originalDefect, err := reader.readDefectQualificationPreparation(ctx, who, fixture.Tenant, defectQ.Report, defectExpected)
			if err != nil || originalDefect.report.ref != defectQ.Report {
				t.Fatal("original defect qualification preparation", err)
			}
			if originalDefect.metrics.PeakRSSBytes != 401 || originalDefect.metrics.PeakVRAMBytes != 0 || originalDefect.metrics.ScratchBytes != 503 {
				t.Fatal("original defect resource metrics lost", originalDefect.metrics)
			}
			t.Log("V27_DEFECT_QUALIFICATION_RESOURCE_METRICS_OK")
			defectCosts := v2input.SemanticQualifiedCosts{RAMBytes: 401, ScratchBytes: 503}
			costQualified, err := reader.readDefectExecutionQualification(ctx, who, fixture.Tenant, defectQ.Report, defectExpected, defectCosts)
			if err != nil || costQualified != originalDefect {
				t.Fatal("defect measured costs qualification", err)
			}
			for _, badCosts := range []v2input.SemanticQualifiedCosts{{RAMBytes: 400, ScratchBytes: 503}, {RAMBytes: 401, ScratchBytes: 502}} {
				denied, err := reader.readDefectExecutionQualification(ctx, who, fixture.Tenant, defectQ.Report, defectExpected, badCosts)
				if err != v2input.ErrInvalid || denied != (defectQualificationPreparationProof{}) {
					t.Fatal("understated defect costs retained qualification", err)
				}
			}
			t.Log("V27_DEFECT_MEASURED_COST_DENIAL_OK")
			profileParams := PublishedDefectParams{Ref: fixture.DefectPins.Params, Inputs: []review.ArtifactRef{{ID: request.Intent.Reference.Source.ArtifactID, SHA256: request.Intent.Reference.Source.SHA256}}}
			profileSizes := make([]v2input.SemanticInputSize, 0, 2)
			for _, ref := range []v2input.Ref{profileParams.Ref, request.Intent.Reference.Source} {
				var storedSHA string
				var storedBytes int64
				if err := evidenceOwner.QueryRow(ctx, "SELECT sha256,bytes FROM photo.artifacts WHERE tenant_id=$1 AND id=$2 AND verified_at IS NOT NULL", fixture.Tenant, ref.ArtifactID).Scan(&storedSHA, &storedBytes); err != nil || storedSHA != ref.SHA256 {
					t.Fatal("profile fixture catalog", err)
				}
				profileSizes = append(profileSizes, v2input.SemanticInputSize{Ref: ref, Bytes: storedBytes})
			}
			qualifiedProfile, err := reader.compileDefectExecutionProfile(ctx, who, fixture.Tenant, originalDefect, defectExpected, 24, 20, profileParams, profileSizes, defectCosts, 1<<40, 0, 1<<40, 1<<40)
			if err != nil || len(qualifiedProfile) == 0 {
				t.Fatal("original defect qualified profile", err)
			}
			underCosts := defectCosts
			underCosts.ScratchBytes--
			if denied, err := reader.compileDefectExecutionProfile(ctx, who, fixture.Tenant, originalDefect, defectExpected, 24, 20, profileParams, profileSizes, underCosts, 1<<40, 0, 1<<40, 1<<40); err != v2input.ErrInvalid || denied != nil {
				t.Fatal("original defect understated profile", err)
			}
			t.Log("V27_DEFECT_ORIGINAL_QUALIFIED_PROFILE_OK")
			originalProfileCatalog := defectProfileCatalog{reader: reader, binding: request.Intent.Reference, pins: fixture.DefectPins}
			originalProfileNodes := []v2draft.LayerRef{{Ref: profileParams.Ref, Contract: "Params"}, {Ref: request.Intent.Reference.Source, Contract: "WorkingImage", CanvasHash: request.Intent.Reference.CanvasHash}}
			catalogProfile, err := reader.prepareCatalogDefectExecutionProfile(ctx, originalProfileCatalog, who, fixture.Tenant, originalDefect, defectExpected, 24, 20, profileParams, originalProfileNodes, defectCosts, 1<<40, 0, 1<<40, 1<<40)
			if err != nil || !bytes.Equal(catalogProfile, qualifiedProfile) {
				t.Fatal("authorized original defect profile catalog", err)
			}
			wrongParamsNode := originalProfileNodes[0]
			wrongParamsNode.Ref = defectQ.Report
			if _, err := originalProfileCatalog.Lookup(ctx, who, fixture.Tenant, wrongParamsNode); err != v2input.ErrInvalid {
				t.Fatal("foreign Params catalog accepted", err)
			}
			t.Log("V27_DEFECT_AUTHORIZED_PROFILE_CATALOG_OK")
			profileLandmarkNode := v2draft.LayerRef{Ref: fixture.DefectLandmark, Contract: "FaceLandmarks", CanvasHash: request.Intent.Reference.CanvasHash}
			profileLandmarkObject, err := originalProfileCatalog.Lookup(ctx, who, fixture.Tenant, profileLandmarkNode)
			if err != nil || profileLandmarkObject.SHA256 != fixture.DefectLandmark.SHA256 {
				t.Fatal("profile landmark catalog", err)
			}
			wrongProfileLandmark := profileLandmarkNode
			wrongProfileLandmark.CanvasHash = strings.Repeat("0", 64)
			if _, err := originalProfileCatalog.Lookup(ctx, who, fixture.Tenant, wrongProfileLandmark); err != v2input.ErrInvalid {
				t.Fatal("foreign landmark canvas accepted", err)
			}
			wrongProfileLandmark = profileLandmarkNode
			wrongProfileLandmark.Ref = fixture.Protection
			if _, err := originalProfileCatalog.Lookup(ctx, who, fixture.Tenant, wrongProfileLandmark); err == nil {
				t.Fatal("non-landmark profile artifact accepted")
			}
			t.Log("V27_DEFECT_PROFILE_LANDMARK_CATALOG_OK")
			landmarkCache := map[uuid.UUID]verifiedLandmarkDocument{}
			originalLandmarkBody, err := reader.readDefectLandmarkDocument(ctx, landmarkCache, profileLandmarkNode.Ref, profileLandmarkObject)
			if err != nil || len(originalLandmarkBody) == 0 {
				t.Fatal("original landmark bytes", err)
			}
			currentLandmarkObject, err := originalProfileCatalog.Lookup(ctx, who, fixture.Tenant, profileLandmarkNode)
			if err != nil {
				t.Fatal("landmark repeated authorization", err)
			}
			reusedLandmarkBody, err := reader.readDefectLandmarkDocument(ctx, landmarkCache, profileLandmarkNode.Ref, currentLandmarkObject)
			if err != nil || !bytes.Equal(originalLandmarkBody, reusedLandmarkBody) {
				t.Fatal("original landmark reuse", err)
			}
			reusedLandmarkBody[0] ^= 1
			retainedLandmarkBody, err := reader.readDefectLandmarkDocument(ctx, landmarkCache, profileLandmarkNode.Ref, currentLandmarkObject)
			if err != nil || !bytes.Equal(originalLandmarkBody, retainedLandmarkBody) {
				t.Fatal("original landmark ownership", err)
			}
			corruptLandmark := &corruptDefectOpen{delegate: reader.Objects, key: profileLandmarkObject.Key}
			corruptLandmarkReader := reader
			corruptLandmarkReader.Objects = corruptLandmark
			corruptLandmarkCache := map[uuid.UUID]verifiedLandmarkDocument{}
			if denied, err := corruptLandmarkReader.readDefectLandmarkDocument(ctx, corruptLandmarkCache, profileLandmarkNode.Ref, profileLandmarkObject); err == nil || denied != nil || len(corruptLandmarkCache) != 0 || !corruptLandmark.done {
				t.Fatal("corrupt original landmarks accepted", err)
			}
			t.Log("V27_DEFECT_PROFILE_ORIGINAL_LANDMARK_BYTES_OK")

			fullSourceParams := PublishedDefectParams{Ref: profileParams.Ref}
			fullSourceNodes := []v2draft.LayerRef{{Ref: profileParams.Ref, Contract: "Params"}}
			for _, pin := range sizePins {
				fullSourceParams.Inputs = append(fullSourceParams.Inputs, review.ArtifactRef{ID: pin.Node.Ref.ArtifactID, SHA256: pin.Node.Ref.SHA256})
				fullSourceNodes = append(fullSourceNodes, pin.Node)
			}
			requiredProfileMask, err := reader.ReadProtection(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.RequiredProtection)
			if err != nil {
				t.Fatal("original Params permanent protection", err)
			}
			discoveredProtection, err := reader.readDiscoveredProtection(ctx, who, fixture.Tenant, request.Intent.Reference, requiredProfileMask.Pixels)
			if err != nil || discoveredProtection.Root != requiredProfileMask.Root || discoveredProtection.Pixels != requiredProfileMask.Pixels || !bytes.Equal(discoveredProtection.Body, requiredProfileMask.Body) {
				t.Fatal("protection root discovery", err)
			}
			t.Log("V27_PROTECTION_ROOT_DISCOVERY_OK")
			fullSourceParams.Inputs = append(fullSourceParams.Inputs, review.ArtifactRef{ID: requiredProfileMask.Root.ArtifactID, SHA256: requiredProfileMask.Root.SHA256}, review.ArtifactRef{ID: requiredProfileMask.Pixels.ArtifactID, SHA256: requiredProfileMask.Pixels.SHA256})
			fullSourceNodes = append(fullSourceNodes, v2draft.LayerRef{Ref: requiredProfileMask.Root, Contract: "MaskRef", CanvasHash: request.Intent.Reference.CanvasHash}, v2draft.LayerRef{Ref: requiredProfileMask.Pixels, Contract: "MaskPixels", ParentRef: requiredProfileMask.Root})
			preparedSourceExecution, err := reader.prepareSourceDefectExecutionEvidence(ctx, originalProfileCatalog, who, fixture.Tenant, request.Intent.Reference, color, defectQ.Report, defectExpected, fullSourceParams, fullSourceNodes, defectCosts, 1<<40, 0, 1<<40, 1<<40)
			sourceProfile := json.RawMessage(preparedSourceExecution.profile)
			if err != nil || len(sourceProfile) == 0 {
				t.Fatal("source-derived defect profile", err)
			}
			if preparedSourceExecution.source != retainedSourceProof || preparedSourceExecution.qualification != originalDefect || preparedSourceExecution.descriptors == "" || preparedSourceExecution.expected == "" {
				t.Fatal("source execution preparation lost original proofs")
			}
			var retainedDescriptors struct {
				Published PublishedDefectParams
				Nodes     []v2draft.LayerRef
			}
			if json.Unmarshal([]byte(preparedSourceExecution.descriptors), &retainedDescriptors) != nil || retainedDescriptors.Published.Ref != fullSourceParams.Ref || len(retainedDescriptors.Nodes) != len(fullSourceNodes) || len(retainedDescriptors.Published.Inputs) != len(fullSourceParams.Inputs) {
				t.Fatal("source execution descriptor retention")
			}
			t.Log("V27_DEFECT_SOURCE_EXECUTION_RETAINED_PROOFS_OK")
			var retainedFiles []v2draft.InputEvidence
			if json.Unmarshal([]byte(preparedSourceExecution.files), &retainedFiles) != nil || len(retainedFiles) != len(fullSourceNodes) {
				t.Fatal("retained file inventory missing")
			}
			for i, entry := range retainedFiles {
				if entry.Ref != fullSourceNodes[i].Ref || entry.Contract != fullSourceNodes[i].Contract {
					t.Fatal("retained file descriptor mismatch", i)
				}
				var stored objectstore.Object
				if err := evidenceOwner.QueryRow(ctx, "SELECT object_key,object_version,sha256,bytes,media_type FROM photo.artifacts WHERE tenant_id=$1 AND id=$2", fixture.Tenant, entry.Ref.ArtifactID).Scan(&stored.Key, &stored.VersionID, &stored.SHA256, &stored.Bytes, &stored.ContentType); err != nil || stored != entry.Object {
					t.Fatal("retained original file metadata mismatch", i, err)
				}
			}
			t.Log("V27_DEFECT_SOURCE_EXECUTION_FILE_PINS_OK")
			if decoded, err := reader.decodeDefectExecutionFilePins(ctx, who, fixture.Tenant, request.Intent.Reference, preparedSourceExecution); err != nil || len(decoded) != len(retainedFiles) {
				t.Fatal("original file pins preflight", err)
			}
			changedFiles := append([]v2draft.InputEvidence(nil), retainedFiles...)
			for i := range changedFiles {
				if changedFiles[i].Ref == request.Intent.Reference.Source {
					changedFiles[i].Object.VersionID = "changed-version"
				}
			}
			changedFileBody, err := canonical.Marshal(changedFiles)
			if err != nil {
				t.Fatal(err)
			}
			changedFilePreparation := preparedSourceExecution
			changedFilePreparation.files = string(changedFileBody)
			if decoded, err := reader.decodeDefectExecutionFilePins(ctx, who, fixture.Tenant, request.Intent.Reference, changedFilePreparation); err != v2input.ErrConflict || decoded != nil {
				t.Fatal("changed source file pins accepted", err)
			}
			duplicateFiles := append([]v2draft.InputEvidence(nil), retainedFiles...)
			duplicateFiles[1] = duplicateFiles[0]
			duplicateFileBody, err := canonical.Marshal(duplicateFiles)
			if err != nil {
				t.Fatal(err)
			}
			changedFilePreparation.files = string(duplicateFileBody)
			if decoded, err := reader.decodeDefectExecutionFilePins(ctx, who, fixture.Tenant, request.Intent.Reference, changedFilePreparation); err != v2input.ErrInvalid || decoded != nil {
				t.Fatal("duplicate retained file pins accepted", err)
			}
			t.Log("V27_DEFECT_SOURCE_EXECUTION_FILE_PIN_MUTATION_DENIED_OK")

			var parsedSourceProfile struct {
				Estimate struct {
					Pixels int64 `json:"pixel_count"`
				} `json:"resource_estimate"`
			}
			if json.Unmarshal(sourceProfile, &parsedSourceProfile) != nil || parsedSourceProfile.Estimate.Pixels != preparedSource.Width*preparedSource.Height {
				t.Fatal("profile dimensions not source-derived")
			}
			if denied, err := reader.prepareSourceDefectExecutionProfile(ctx, originalProfileCatalog, who, fixture.Tenant, request.Intent.Reference, color, defectQ.Report, defectExpected, profileParams, originalProfileNodes, defectCosts, 1<<40, 0, 1<<40, 1<<40); err != v2input.ErrInvalid || denied != nil {
				t.Fatal("missing RGB closure accepted", err)
			}
			t.Log("V27_DEFECT_SOURCE_DERIVED_PROFILE_OK")
			wrongMaskContractNodes := append([]v2draft.LayerRef(nil), fullSourceNodes...)
			for i := range wrongMaskContractNodes {
				if wrongMaskContractNodes[i].Ref == requiredProfileMask.Pixels {
					wrongMaskContractNodes[i].Contract = "Artifact"
				}
			}
			if denied, err := reader.prepareSourceDefectExecutionProfile(ctx, originalProfileCatalog, who, fixture.Tenant, request.Intent.Reference, color, defectQ.Report, defectExpected, fullSourceParams, wrongMaskContractNodes, defectCosts, 1<<40, 0, 1<<40, 1<<40); err == nil || denied != nil {
				t.Fatal("untyped mask profile dependency accepted", err)
			}
			multiProfileParams := PublishedDefectParams{Ref: fixture.MultiFaceDefectPins.Params, Inputs: append([]review.ArtifactRef(nil), fullSourceParams.Inputs...)}
			multiProfileNodes := append([]v2draft.LayerRef(nil), fullSourceNodes...)
			multiProfileNodes[0].Ref = multiProfileParams.Ref
			for _, landmark := range []v2input.Ref{fixture.DefectLandmark, fixture.SecondDefectLandmark} {
				multiProfileParams.Inputs = append(multiProfileParams.Inputs, review.ArtifactRef{ID: landmark.ArtifactID, SHA256: landmark.SHA256})
				multiProfileNodes = append(multiProfileNodes, v2draft.LayerRef{Ref: landmark, Contract: "FaceLandmarks", CanvasHash: request.Intent.Reference.CanvasHash})
			}
			multiProfileCatalog := defectProfileCatalog{reader: reader, binding: request.Intent.Reference, pins: fixture.MultiFaceDefectPins}
			if profile, err := reader.prepareSourceDefectExecutionProfile(ctx, multiProfileCatalog, who, fixture.Tenant, request.Intent.Reference, color, defectQ.Report, defectExpected, multiProfileParams, multiProfileNodes, defectCosts, 1<<40, 0, 1<<40, 1<<40); err != nil || len(profile) == 0 {
				t.Fatal("multi-face original source profile", err)
			}
			t.Log("V27_DEFECT_PROFILE_MULTIFACE_SOURCE_OK")

			originalFaces, originalFaceProofs, err := reader.readQualifiedDefectFaceEvidence(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.MultiFaceDefectPins, faceQualificationReports, qualifiedProfiles)
			if err != nil || len(originalFaces) != 2 || len(originalFaceProofs) != 2 {
				t.Fatal("multi-face original producer proof set", err)
			}
			for _, proof := range originalFaceProofs {
				if !validFacePreparationProofScope(reader, who, fixture.Tenant, request.Intent.Reference, proof) {
					t.Fatal("invalid original face proof scope")
				}
				if err := reader.recheckFacePreparationProof(ctx, who, fixture.Tenant, request.Intent.Reference, proof); err != nil {
					t.Fatal("original face proof recheck", err)
				}
			}
			t.Log("V27_DEFECT_FACE_ORIGINAL_PRODUCER_PROOFS_OK")
			preparedFaceExecution, err := reader.prepareFaceQualifiedDefectExecutionEvidence(ctx, who, fixture.Tenant, request.Intent.Reference, color, fixture.MultiFaceDefectPins, faceQualificationReports, qualifiedProfiles, defectQ.Report, defectExpected, multiProfileParams, multiProfileNodes, defectCosts, 1<<40, 0, 1<<40, 1<<40)
			if err != nil || preparedFaceExecution.source.profile == "" || preparedFaceExecution.origin != fixture.MultiFaceDefectPins || preparedFaceExecution.source.source != retainedSourceProof || preparedFaceExecution.source.qualification != originalDefect {
				t.Fatal("retained face execution preparation", err)
			}

			if preparedFaceExecution.producerCount != len(originalFaceProofs) {
				t.Fatal("producer proof retention count")
			}
			for i, proof := range originalFaceProofs {
				if preparedFaceExecution.producers[i] != proof {
					t.Fatal("original producer proof changed during preparation")
				}
			}
			if preparedFaceExecution.originProof.pins != fixture.MultiFaceDefectPins {
				t.Fatal("retained original defect origin pins")
			}
			if err := reader.recheckDefectOriginPreparationProof(ctx, who, fixture.Tenant, request.Intent.Reference, preparedFaceExecution.originProof); err != nil {
				t.Fatal("retained defect origin metadata", err)
			}
			var retainedFaces []review.Face68
			if json.Unmarshal([]byte(preparedFaceExecution.faces), &retainedFaces) != nil || len(retainedFaces) != 2 || preparedFaceExecution.expected == "" {
				t.Fatal("retained two-face snapshot")
			}
			for i, pin := range preparedFaceExecution.qualification {
				if pin.ref != faceQualificationReports[i] || pin.object.VersionID == "" || pin.object.SHA256 != pin.ref.SHA256 {
					t.Fatal("retained face qualification lost", i)
				}
			}
			t.Log("V27_DEFECT_FACE_EXECUTION_RETAINED_PROOFS_OK")
			unqualifiedFaceProfiles := qualifiedProfiles
			unqualifiedFaceProfiles[0].RuntimeLockSHA = strings.Repeat("d", 64)
			if profile, err := reader.prepareFaceQualifiedDefectExecutionProfile(ctx, who, fixture.Tenant, request.Intent.Reference, color, fixture.MultiFaceDefectPins, faceQualificationReports, unqualifiedFaceProfiles, defectQ.Report, defectExpected, multiProfileParams, multiProfileNodes, defectCosts, 1<<40, 0, 1<<40, 1<<40); err != v2qualification.ErrUnqualified || profile != nil {
				t.Fatal("unqualified face profile composition accepted", err)
			}
			t.Log("V27_DEFECT_PROFILE_FACE_QUALIFIED_OK")
			var finalFaceQualificationKey string
			if err := evidenceOwner.QueryRow(ctx, "SELECT object_key FROM photo.artifacts WHERE tenant_id=$1 AND id=$2", fixture.Tenant, faceQualificationReports[0].ArtifactID).Scan(&finalFaceQualificationKey); err != nil {
				t.Fatal(err)
			}
			finalFaceRevocation := &afterSecondMaskOpen{delegate: reader.Objects, key: finalFaceQualificationKey, at: 2, after: func(ctx context.Context) error {
				_, err := evidenceOwner.Exec(ctx, "UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=$1", defectQ.PolicyID)
				return err
			}}
			finalFaceReader := reader
			finalFaceReader.Objects = finalFaceRevocation
			if profile, err := finalFaceReader.prepareFaceQualifiedDefectExecutionProfile(ctx, who, fixture.Tenant, request.Intent.Reference, color, fixture.MultiFaceDefectPins, faceQualificationReports, qualifiedProfiles, defectQ.Report, defectExpected, multiProfileParams, multiProfileNodes, defectCosts, 1<<40, 0, 1<<40, 1<<40); err != v2qualification.ErrUnqualified || profile != nil || !finalFaceRevocation.done {
				t.Fatal("defect revocation during final face pass accepted", err, finalFaceRevocation.opens)
			}
			if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.v2_qualification_policies SET admission_enabled=true WHERE id=$1", defectQ.PolicyID); err != nil {
				t.Fatal(err)
			}
			if profile, err := reader.prepareFaceQualifiedDefectExecutionProfile(ctx, who, fixture.Tenant, request.Intent.Reference, color, fixture.MultiFaceDefectPins, faceQualificationReports, qualifiedProfiles, defectQ.Report, defectExpected, multiProfileParams, multiProfileNodes, defectCosts, 1<<40, 0, 1<<40, 1<<40); err != nil || len(profile) == 0 {
				t.Fatal("restored composed qualification", err)
			}
			t.Log("V27_DEFECT_PROFILE_FINAL_FACE_REVOCATION_OK")
			var finalSourcePixel uuid.UUID
			for _, pin := range sizePins {
				if pin.Node.Contract == "WorkingPixels" {
					finalSourcePixel = pin.Node.Ref.ArtifactID
				}
			}
			if finalSourcePixel == uuid.Nil {
				t.Fatal("source pixel fixture missing")
			}
			finalSourceExpiry := &afterSecondMaskOpen{delegate: reader.Objects, key: finalFaceQualificationKey, at: 2, after: func(ctx context.Context) error {
				_, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 day' WHERE tenant_id=$1 AND id=$2", fixture.Tenant, finalSourcePixel)
				return err
			}}
			finalSourceReader := reader
			finalSourceReader.Objects = finalSourceExpiry
			if profile, err := finalSourceReader.prepareFaceQualifiedDefectExecutionProfile(ctx, who, fixture.Tenant, request.Intent.Reference, color, fixture.MultiFaceDefectPins, faceQualificationReports, qualifiedProfiles, defectQ.Report, defectExpected, multiProfileParams, multiProfileNodes, defectCosts, 1<<40, 0, 1<<40, 1<<40); err == nil || profile != nil || !finalSourceExpiry.done {
				t.Fatal("source expiry during final face pass accepted", err, finalSourceExpiry.opens)
			}
			if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=NULL WHERE tenant_id=$1 AND id=$2", fixture.Tenant, finalSourcePixel); err != nil {
				t.Fatal(err)
			}
			if profile, err := reader.prepareFaceQualifiedDefectExecutionProfile(ctx, who, fixture.Tenant, request.Intent.Reference, color, fixture.MultiFaceDefectPins, faceQualificationReports, qualifiedProfiles, defectQ.Report, defectExpected, multiProfileParams, multiProfileNodes, defectCosts, 1<<40, 0, 1<<40, 1<<40); err != nil || len(profile) == 0 {
				t.Fatal("restored composed source", err)
			}
			t.Log("V27_DEFECT_PROFILE_FINAL_FACE_SOURCE_EXPIRY_OK")
			skinProfileReports := [2]v2input.Ref{fixture.ParseQualification.Report, faceQualificationReports[1]}
			skinProfileExpected := [2]v2qualification.Expected{parseExpected, qualifiedProfiles[1]}
			skinProfileRoots, originalParserProofs, err := reader.readQualifiedDefectFaceMaskEvidence(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.SkinFaceDefectPins, skinProfileReports, skinProfileExpected, color)
			if err != nil || len(skinProfileRoots) != 1 {
				t.Fatal("profile original skin roots", err)
			}
			skinProfileParams := PublishedDefectParams{Ref: fixture.SkinFaceDefectPins.Params, Inputs: append([]review.ArtifactRef(nil), fullSourceParams.Inputs...)}
			skinProfileNodes := append([]v2draft.LayerRef(nil), fullSourceNodes...)
			skinProfileNodes[0].Ref = skinProfileParams.Ref
			for _, node := range []v2draft.LayerRef{
				{Ref: fixture.DefectLandmark, Contract: "FaceLandmarks", CanvasHash: request.Intent.Reference.CanvasHash},
				{Ref: fixture.SkinMaskRoot, Contract: "MaskRef", CanvasHash: request.Intent.Reference.CanvasHash},
				{Ref: v2input.Ref{ArtifactID: skinProfileRoots[0].Binding.Mask.Artifact.ID, SHA256: skinProfileRoots[0].Binding.Mask.Artifact.SHA256}, Contract: "MaskPixels", ParentRef: fixture.SkinMaskRoot},
			} {
				skinProfileParams.Inputs = append(skinProfileParams.Inputs, review.ArtifactRef{ID: node.Ref.ArtifactID, SHA256: node.Ref.SHA256})
				skinProfileNodes = append(skinProfileNodes, node)
			}
			preparedMaskExecution, err := reader.prepareMaskQualifiedDefectExecutionEvidence(ctx, who, fixture.Tenant, request.Intent.Reference, color, fixture.SkinFaceDefectPins, faceQualificationReports, skinProfileReports, qualifiedProfiles, skinProfileExpected, defectQ.Report, defectExpected, skinProfileParams, skinProfileNodes, defectCosts, 1<<40, 0, 1<<40, 1<<40)
			if err != nil || preparedMaskExecution.face.source.profile == "" || preparedMaskExecution.face.origin != fixture.SkinFaceDefectPins || preparedMaskExecution.face.source.source != retainedSourceProof || preparedMaskExecution.face.source.qualification != originalDefect {
				t.Fatal("retained mask execution preparation", err)
			}
			var retainedMaskRoots []review.ProposalFaceMaskRoot
			if json.Unmarshal([]byte(preparedMaskExecution.roots), &retainedMaskRoots) != nil || len(retainedMaskRoots) != 1 || retainedMaskRoots[0].Root != skinProfileRoots[0].Root || preparedMaskExecution.expected == "" {
				t.Fatal("retained mask root inventory")
			}
			for i, ref := range [4]v2input.Ref{faceQualificationReports[0], faceQualificationReports[1], skinProfileReports[0], skinProfileReports[1]} {
				if preparedMaskExecution.qualification[i].ref != ref || preparedMaskExecution.qualification[i].object.VersionID == "" {
					t.Fatal("retained mask qualification lost", i)
				}
			}
			t.Log("V27_DEFECT_MASK_EXECUTION_RETAINED_PROOFS_OK")
			badSkinExpected := skinProfileExpected
			badSkinExpected[0].RuntimeLockSHA = strings.Repeat("d", 64)
			if profile, err := reader.prepareMaskQualifiedDefectExecutionProfile(ctx, who, fixture.Tenant, request.Intent.Reference, color, fixture.SkinFaceDefectPins, faceQualificationReports, skinProfileReports, qualifiedProfiles, badSkinExpected, defectQ.Report, defectExpected, skinProfileParams, skinProfileNodes, defectCosts, 1<<40, 0, 1<<40, 1<<40); err == nil || profile != nil {
				t.Fatal("unqualified parser profile accepted", err)
			}
			if len(originalParserProofs) != 1 {
				t.Fatal("original parser proof count")
			}
			if preparedMaskExecution.parserCount != 1 || preparedMaskExecution.parsers[0] != originalParserProofs[0] {
				t.Fatal("retained original parser proof")
			}
			if err := reader.recheckFaceParsePreparationProof(ctx, who, fixture.Tenant, request.Intent.Reference, preparedMaskExecution.parsers[0]); err != nil {
				t.Fatal("retained original parser proof recheck", err)
			}
			t.Log("V27_DEFECT_MASK_ORIGINAL_PARSER_PROOFS_OK")
			t.Log("V27_DEFECT_PROFILE_SKIN_QUALIFIED_OK")
			missingSkinRootParams := PublishedDefectParams{Ref: skinProfileParams.Ref}
			var missingSkinRootNodes []v2draft.LayerRef
			for _, ref := range skinProfileParams.Inputs {
				if ref.ID != fixture.SkinMaskRoot.ArtifactID {
					missingSkinRootParams.Inputs = append(missingSkinRootParams.Inputs, ref)
				}
			}
			for _, node := range skinProfileNodes {
				if node.Ref != fixture.SkinMaskRoot {
					missingSkinRootNodes = append(missingSkinRootNodes, node)
				}
			}
			if profile, err := reader.prepareMaskQualifiedDefectExecutionProfile(ctx, who, fixture.Tenant, request.Intent.Reference, color, fixture.SkinFaceDefectPins, faceQualificationReports, skinProfileReports, qualifiedProfiles, skinProfileExpected, defectQ.Report, defectExpected, missingSkinRootParams, missingSkinRootNodes, defectCosts, 1<<40, 0, 1<<40, 1<<40); err != v2input.ErrInvalid || profile != nil {
				t.Fatal("unaccounted original skin root accepted", err)
			}
			t.Log("V27_DEFECT_PROFILE_SKIN_ROOT_REQUIRED_OK")
			var finalParserQualificationKey string
			if err := evidenceOwner.QueryRow(ctx, "SELECT object_key FROM photo.artifacts WHERE tenant_id=$1 AND id=$2", fixture.Tenant, fixture.ParseQualification.Report.ArtifactID).Scan(&finalParserQualificationKey); err != nil {
				t.Fatal(err)
			}
			for _, policyID := range []uuid.UUID{fixture.FaceQualifications[0].PolicyID, fixture.ParseQualification.PolicyID, fixture.FaceQualifications[1].PolicyID} {
				finalMaskFaceRevocation := &afterSecondMaskOpen{delegate: reader.Objects, key: finalParserQualificationKey, at: 2, after: func(ctx context.Context) error {
					_, err := evidenceOwner.Exec(ctx, "UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=$1", policyID)
					return err
				}}
				finalMaskFaceReader := reader
				finalMaskFaceReader.Objects = finalMaskFaceRevocation
				if profile, err := finalMaskFaceReader.prepareMaskQualifiedDefectExecutionProfile(ctx, who, fixture.Tenant, request.Intent.Reference, color, fixture.SkinFaceDefectPins, faceQualificationReports, skinProfileReports, qualifiedProfiles, skinProfileExpected, defectQ.Report, defectExpected, skinProfileParams, skinProfileNodes, defectCosts, 1<<40, 0, 1<<40, 1<<40); err != v2qualification.ErrUnqualified || profile != nil || !finalMaskFaceRevocation.done {
					t.Fatal("policy revoked during final mask pass accepted", policyID, err)
				}
				if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.v2_qualification_policies SET admission_enabled=true WHERE id=$1", policyID); err != nil {
					t.Fatal(err)
				}
			}
			t.Log("V27_DEFECT_PROFILE_FINAL_MASK_FACE_REVOCATION_OK")
			t.Log("V27_DEFECT_PROFILE_FINAL_MASK_PARSER_DETECTOR_REVOCATION_OK")
			var finalLandmarkProducer uuid.UUID
			if err := evidenceOwner.QueryRow(ctx, "SELECT created_by_task_id FROM photo.artifacts WHERE tenant_id=$1 AND id=$2", fixture.Tenant, fixture.DefectLandmark.ArtifactID).Scan(&finalLandmarkProducer); err != nil {
				t.Fatal(err)
			}
			finalMaskProducerChange := &afterSecondMaskOpen{delegate: reader.Objects, key: finalParserQualificationKey, at: 2, after: func(ctx context.Context) error {
				_, err := evidenceOwner.Exec(ctx, "UPDATE photo.task_attempts SET profile=jsonb_set(profile,'{runtime_lock_sha}',to_jsonb($2::text)) WHERE tenant_id=$3 AND task_id=$1", finalLandmarkProducer, strings.Repeat("d", 64), fixture.Tenant)
				return err
			}}
			finalProducerReader := reader
			finalProducerReader.Objects = finalMaskProducerChange
			if profile, err := finalProducerReader.prepareMaskQualifiedDefectExecutionProfile(ctx, who, fixture.Tenant, request.Intent.Reference, color, fixture.SkinFaceDefectPins, faceQualificationReports, skinProfileReports, qualifiedProfiles, skinProfileExpected, defectQ.Report, defectExpected, skinProfileParams, skinProfileNodes, defectCosts, 1<<40, 0, 1<<40, 1<<40); err != v2qualification.ErrUnqualified || profile != nil || !finalMaskProducerChange.done {
				t.Fatal("changed landmark producer during final mask pass accepted", err)
			}
			if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.task_attempts SET profile=jsonb_set(profile,'{runtime_lock_sha}',to_jsonb($2::text)) WHERE tenant_id=$3 AND task_id=$1", finalLandmarkProducer, qualifiedProfiles[0].RuntimeLockSHA, fixture.Tenant); err != nil {
				t.Fatal(err)
			}
			t.Log("V27_DEFECT_PROFILE_FINAL_MASK_PRODUCER_CHANGE_OK")

			missingProtectionParams := PublishedDefectParams{Ref: fullSourceParams.Ref}
			missingProtectionNodes := make([]v2draft.LayerRef, 0, len(fullSourceNodes)-1)
			for _, ref := range fullSourceParams.Inputs {
				if ref.ID != requiredProfileMask.Pixels.ArtifactID {
					missingProtectionParams.Inputs = append(missingProtectionParams.Inputs, ref)
				}
			}
			for _, node := range fullSourceNodes {
				if node.Ref.ArtifactID != requiredProfileMask.Pixels.ArtifactID {
					missingProtectionNodes = append(missingProtectionNodes, node)
				}
			}
			if denied, err := reader.prepareSourceDefectExecutionProfile(ctx, originalProfileCatalog, who, fixture.Tenant, request.Intent.Reference, color, defectQ.Report, defectExpected, missingProtectionParams, missingProtectionNodes, defectCosts, 1<<40, 0, 1<<40, 1<<40); err != v2input.ErrInvalid || denied != nil {
				t.Fatal("Params permanent protection omission accepted", err)
			}
			staleProtectionParams := PublishedDefectParams{Ref: fullSourceParams.Ref, Inputs: append([]review.ArtifactRef(nil), fullSourceParams.Inputs...)}
			staleProtectionNodes := append([]v2draft.LayerRef(nil), fullSourceNodes...)
			for i := range staleProtectionParams.Inputs {
				if staleProtectionParams.Inputs[i].ID == requiredProfileMask.Pixels.ArtifactID {
					staleProtectionParams.Inputs[i].SHA256 = strings.Repeat("0", 64)
				}
			}
			for i := range staleProtectionNodes {
				if staleProtectionNodes[i].Ref.ArtifactID == requiredProfileMask.Pixels.ArtifactID {
					staleProtectionNodes[i].Ref.SHA256 = strings.Repeat("0", 64)
				}
			}
			if denied, err := reader.prepareSourceDefectExecutionProfile(ctx, originalProfileCatalog, who, fixture.Tenant, request.Intent.Reference, color, defectQ.Report, defectExpected, staleProtectionParams, staleProtectionNodes, defectCosts, 1<<40, 0, 1<<40, 1<<40); err != v2input.ErrInvalid || denied != nil {
				t.Fatal("Params permanent protection stale SHA accepted", err)
			}
			t.Log("V27_DEFECT_PROFILE_REQUIRED_PROTECTION_OK")
			originalProfileParamsObject, err := originalProfileCatalog.Lookup(ctx, who, fixture.Tenant, fullSourceNodes[0])
			if err != nil {
				t.Fatal("original profile Params object", err)
			}
			corruptProfileParams := &corruptDefectOpen{delegate: objects, key: originalProfileParamsObject.Key}
			corruptProfileReader := reader
			corruptProfileReader.Objects = corruptProfileParams
			if denied, err := corruptProfileReader.prepareSourceDefectExecutionProfile(ctx, originalProfileCatalog, who, fixture.Tenant, request.Intent.Reference, color, defectQ.Report, defectExpected, fullSourceParams, fullSourceNodes, defectCosts, 1<<40, 0, 1<<40, 1<<40); err == nil || denied != nil || !corruptProfileParams.done {
				t.Fatal("corrupt original Params profile accepted", err, corruptProfileParams.done)
			}
			restoredParamsProfile, err := reader.prepareSourceDefectExecutionProfile(ctx, originalProfileCatalog, who, fixture.Tenant, request.Intent.Reference, color, defectQ.Report, defectExpected, fullSourceParams, fullSourceNodes, defectCosts, 1<<40, 0, 1<<40, 1<<40)
			if err != nil || !bytes.Equal(restoredParamsProfile, sourceProfile) {
				t.Fatal("original Params profile recovery", err)
			}
			t.Log("V27_DEFECT_PROFILE_ORIGINAL_PARAMS_BYTES_OK")
			profileMaskObject, err := originalProfileCatalog.Lookup(ctx, who, fixture.Tenant, fullSourceNodes[len(fullSourceNodes)-1])
			if err != nil {
				t.Fatal("original profile mask object", err)
			}
			corruptProfileMask := &corruptDefectOpen{delegate: objects, key: profileMaskObject.Key}
			corruptMaskProfileReader := reader
			corruptMaskProfileReader.Objects = corruptProfileMask
			if denied, err := corruptMaskProfileReader.prepareSourceDefectExecutionProfile(ctx, originalProfileCatalog, who, fixture.Tenant, request.Intent.Reference, color, defectQ.Report, defectExpected, fullSourceParams, fullSourceNodes, defectCosts, 1<<40, 0, 1<<40, 1<<40); err == nil || denied != nil || !corruptProfileMask.done {
				t.Fatal("corrupt Params mask profile accepted", err, corruptProfileMask.done)
			}
			t.Log("V27_DEFECT_PROFILE_ORIGINAL_MASK_BYTES_OK")
			budgetMaskProbe := &corruptDefectOpen{delegate: objects, key: profileMaskObject.Key}
			budgetProfileReader := reader
			budgetProfileReader.Objects = budgetMaskProbe
			if denied, err := budgetProfileReader.prepareSourceDefectExecutionProfile(ctx, originalProfileCatalog, who, fixture.Tenant, request.Intent.Reference, color, defectQ.Report, defectExpected, fullSourceParams, fullSourceNodes, defectCosts, 1<<30, 0, 1<<40, 1<<40); err != v2input.ErrInvalid || denied != nil || budgetMaskProbe.done {
				t.Fatal("defect mask opened before capacity refusal", err, budgetMaskProbe.done)
			}
			t.Log("V27_DEFECT_PROFILE_CAPACITY_BEFORE_MASK_IO_OK")
			revokingProfileCatalog := &afterDefectProfileLookup{delegate: originalProfileCatalog, after: func(ctx context.Context) error {
				_, err := evidenceOwner.Exec(ctx, "UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=$1", defectQ.PolicyID)
				return err
			}}
			if denied, err := reader.prepareCatalogDefectExecutionProfile(ctx, revokingProfileCatalog, who, fixture.Tenant, originalDefect, defectExpected, 24, 20, profileParams, originalProfileNodes, defectCosts, 1<<40, 0, 1<<40, 1<<40); err != v2qualification.ErrUnqualified || denied != nil || !revokingProfileCatalog.done {
				t.Fatal("qualification revoked during profile catalog accepted", err, revokingProfileCatalog.done)
			}
			if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.v2_qualification_policies SET admission_enabled=true WHERE id=$1", defectQ.PolicyID); err != nil {
				t.Fatal(err)
			}
			recoveredProfile, err := reader.prepareCatalogDefectExecutionProfile(ctx, originalProfileCatalog, who, fixture.Tenant, originalDefect, defectExpected, 24, 20, profileParams, originalProfileNodes, defectCosts, 1<<40, 0, 1<<40, 1<<40)
			if err != nil || !bytes.Equal(recoveredProfile, catalogProfile) {
				t.Fatal("profile after qualification restoration", err)
			}
			t.Log("V27_DEFECT_PROFILE_CATALOG_REVOCATION_OK")
			if err := reader.recheckDefectQualificationPreparation(ctx, who, fixture.Tenant, originalDefect, defectExpected); err != nil {
				t.Fatal("live defect qualification recheck", err)
			}
			if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=$1", defectQ.PolicyID); err != nil {
				t.Fatal(err)
			}
			if err := reader.recheckDefectQualificationPreparation(ctx, who, fixture.Tenant, originalDefect, defectExpected); err != v2qualification.ErrUnqualified {
				t.Fatal("revoked defect qualification recheck accepted", err)
			}
			rejectedDefectQualification, err := reader.readDefectQualificationPreparation(ctx, who, fixture.Tenant, defectQ.Report, defectExpected)
			if err != v2qualification.ErrUnqualified || rejectedDefectQualification != (defectQualificationPreparationProof{}) {
				t.Fatal("revoked defect qualification retained proof", err)
			}
			if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.v2_qualification_policies SET admission_enabled=true WHERE id=$1", defectQ.PolicyID); err != nil {
				t.Fatal(err)
			}
			restoredDefect, err := reader.readDefectQualificationPreparation(ctx, who, fixture.Tenant, defectQ.Report, defectExpected)
			if err != nil || restoredDefect != originalDefect {
				t.Fatal("restored defect qualification mismatch", err)
			}
			if _, err := reader.readDefectQualificationPreparation(ctx, who, fixture.Tenant, fixture.FaceQualifications[0].Report, defectExpected); err != v2qualification.ErrUnqualified {
				t.Fatal("foreign face qualification accepted for defect", err)
			}
			t.Log("V27_DEFECT_ORIGINAL_QUALIFICATION_PREPARATION_OK")
			if err := reader.recheckDefectQualificationPreparation(ctx, who, fixture.Tenant, originalDefect, defectExpected); err != nil {
				t.Fatal("restored defect qualification recheck", err)
			}
			t.Log("V27_DEFECT_QUALIFICATION_FINAL_RECHECK_OK")
			q := fixture.Qualification
			trusted := v2qualification.Expected{Operation: "semantic.transfer", Version: "1", Backend: "cuda", ModelLockSHA: q.ModelLockSHA, RuntimeLockSHA: q.RuntimeLockSHA, DeviceProfileSHA: q.DeviceProfileSHA, DatasetSHA: q.DatasetSHA, IssuerID: q.IssuerID, RequiredChecks: []string{"quality"}, TestFixtureSHAs: map[string]struct{}{strings.Repeat("e", 64): {}}, EvidenceSHAs: map[string]struct{}{strings.Repeat("f", 64): {}}, CalibrationSHAs: map[string]struct{}{q.Calibration.SHA256: {}}, LicenseCleared: true}
			retainedTransfer, err := reader.readTransferQualificationPreparation(ctx, who, fixture.Tenant, q, trusted)
			if err != nil {
				t.Fatal("retained original transfer qualification", err)
			}
			transferLockConfig := cfg.Copy()
			transferLockConfig.ConnConfig.RuntimeParams["role"] = "photo_scheduler"
			transferLockPool, err := pgxpool.NewWithConfig(ctx, transferLockConfig)
			if err != nil {
				t.Fatal(err)
			}
			defer transferLockPool.Close()
			transferLockTx, err := transferLockPool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer transferLockTx.Rollback(ctx)
			if _, err := transferLockTx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", fixture.Tenant.String()); err != nil {
				t.Fatal(err)
			}
			if err := reader.lockDefectQualificationPreparation(ctx, transferLockTx, who, fixture.Tenant, originalDefect, defectExpected); err != nil {
				t.Fatal("original defect qualification lock", err)
			}
			changedDefect := originalDefect
			changedDefect.report.object.VersionID += "-stale"
			if err := reader.lockDefectQualificationPreparation(ctx, transferLockTx, who, fixture.Tenant, changedDefect, defectExpected); err != v2input.ErrConflict {
				t.Fatal("stale defect qualification lock accepted", err)
			}
			t.Log("V27_DEFECT_ORIGINAL_QUALIFICATION_LOCK_OK")
			if err := reader.lockTransferQualificationPreparation(ctx, transferLockTx, who, fixture.Tenant, retainedTransfer, trusted); err != nil {
				t.Fatal("retained transfer qualification lock", err)
			}
			importWriter, err := evidenceOwner.Acquire(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = importWriter.Exec(ctx, "SET lock_timeout='250ms'"); err != nil {
				t.Fatal(err)
			}
			for _, statement := range []string{
				"UPDATE photo.audit_log SET details=details WHERE tenant_id=$1 AND action='v2_qualification.imported' AND resource_id=$2",
				"DELETE FROM photo.audit_log WHERE tenant_id=$1 AND action='v2_qualification.imported' AND resource_id=$2",
			} {
				_, err = importWriter.Exec(ctx, statement, fixture.Tenant, q.QualificationID)
				var blocked *pgconn.PgError
				if !errors.As(err, &blocked) || blocked.Code != "55P03" {
					t.Fatal("import authority mutation crossed transfer lock", err)
				}
			}
			for _, probe := range []struct {
				sql string
				id  uuid.UUID
			}{
				{"UPDATE photo.audit_log SET details=details WHERE tenant_id=$1 AND action='v2_qualification.imported' AND resource_id=$2", defectQ.QualificationID},
				{"DELETE FROM photo.audit_log WHERE tenant_id=$1 AND action='v2_qualification.imported' AND resource_id=$2", defectQ.QualificationID},
				{"UPDATE photo.artifacts SET expires_at=expires_at WHERE tenant_id=$1 AND id=$2", defectQ.Report.ArtifactID},
			} {
				_, err = importWriter.Exec(ctx, probe.sql, fixture.Tenant, probe.id)
				var blocked *pgconn.PgError
				if !errors.As(err, &blocked) || blocked.Code != "55P03" {
					t.Fatal("defect authority mutation crossed lock", err)
				}
			}
			t.Log("V27_DEFECT_QUALIFICATION_AUTHORITY_LOCK_OK")
			importWriter.Exec(context.Background(), "RESET lock_timeout")
			importWriter.Release()
			t.Log("V27_TRANSFER_QUALIFICATION_IMPORT_LOCK_OK")
			for index := range retainedTransfer.objects {
				stale := retainedTransfer
				stale.objects[index].object.VersionID += "-stale"
				if err := reader.lockTransferQualificationPreparation(ctx, transferLockTx, who, fixture.Tenant, stale, trusted); !errors.Is(err, v2input.ErrConflict) {
					t.Fatal("stale transfer qualification version accepted", index, err)
				}
			}
			if err := transferLockTx.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
			t.Log("V27_TRANSFER_QUALIFICATION_RETAINED_LOCK_OK")
			lockRetainedTransfer := func() error {
				tx, err := transferLockPool.Begin(ctx)
				if err != nil {
					return err
				}
				defer tx.Rollback(context.Background())
				if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", fixture.Tenant.String()); err != nil {
					return err
				}
				return reader.lockTransferQualificationPreparation(ctx, tx, who, fixture.Tenant, retainedTransfer, trusted)
			}
			if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=$1", q.PolicyID); err != nil {
				t.Fatal(err)
			}
			if err := lockRetainedTransfer(); !errors.Is(err, v2input.ErrConflict) {
				t.Fatal("revoked transfer qualification accepted", err)
			}
			if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.v2_qualification_policies SET admission_enabled=true WHERE id=$1", q.PolicyID); err != nil {
				t.Fatal(err)
			}
			if err := lockRetainedTransfer(); err != nil {
				t.Fatal("restored transfer qualification rejected", err)
			}
			t.Log("V27_TRANSFER_QUALIFICATION_REVOKE_LOCK_OK")
			for _, role := range []string{"photo_api", "photo_worker", "photo_publisher", "photo_portal", "photo_auth", "photo_gateway", "photo_maintenance", "photo_semantic_admission"} {
				tx, err := evidenceOwner.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = tx.Exec(ctx, "SET LOCAL ROLE "+pgx.Identifier{role}.Sanitize()); err != nil {
					tx.Rollback(context.Background())
					t.Fatal(err)
				}
				_, err = tx.Exec(ctx, "SELECT photo.lock_semantic_proposal_transfer_qualification_evidence(NULL::uuid,NULL::uuid,NULL::uuid,NULL::uuid,NULL::jsonb,NULL::jsonb)")
				tx.Rollback(context.Background())
				var denied *pgconn.PgError
				if !errors.As(err, &denied) || denied.Code != "42501" {
					t.Fatal("transfer evidence gate role not denied", role, err)
				}
			}
			t.Log("V27_TRANSFER_QUALIFICATION_LOCK_ACL_OK")
			for _, role := range []string{"photo_api", "photo_worker", "photo_publisher", "photo_portal", "photo_auth", "photo_gateway", "photo_maintenance", "photo_semantic_admission"} {
				roleTx, err := evidenceOwner.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = roleTx.Exec(ctx, "SET LOCAL ROLE "+pgx.Identifier{role}.Sanitize()); err != nil {
					roleTx.Rollback(context.Background())
					t.Fatal(err)
				}
				_, err = roleTx.Exec(ctx, "SELECT photo.lock_semantic_proposal_defect_qualification(NULL::uuid,NULL::uuid,NULL::uuid,NULL::uuid,NULL::jsonb,NULL::jsonb)")
				roleTx.Rollback(context.Background())
				var denied *pgconn.PgError
				if !errors.As(err, &denied) || denied.Code != "42501" {
					t.Fatal("defect qualification role not denied", role, err)
				}
			}
			t.Log("V27_DEFECT_QUALIFICATION_LOCK_ACL_OK")

			for _, gate := range []string{
				"SELECT photo.lock_semantic_defect_execution_files(NULL::uuid,NULL::uuid,NULL::uuid,NULL::uuid,NULL::uuid,NULL::uuid,NULL::text,NULL::jsonb,NULL::jsonb)",
				"SELECT photo.lock_semantic_defect_origin_evidence(NULL::uuid,NULL::uuid,NULL::uuid,NULL::uuid,NULL::uuid,NULL::uuid,NULL::text,NULL::jsonb)",
				"SELECT photo.lock_semantic_defect_execution_scope(NULL::uuid,NULL::uuid,NULL::uuid,NULL::uuid,NULL::uuid,NULL::uuid,NULL::text)",
				"SELECT photo.lock_semantic_defect_version_params(NULL::uuid,NULL::uuid,NULL::uuid,NULL::uuid,NULL::uuid,NULL::uuid,NULL::text,NULL::uuid,NULL::uuid,NULL::text,NULL::jsonb)",
				"SELECT photo.semantic_defect_preparation_registration_state(NULL::uuid,NULL::uuid,NULL::uuid,NULL::uuid,NULL::text,NULL::text,NULL::jsonb,NULL::jsonb,NULL::jsonb,NULL::jsonb,NULL::jsonb,NULL::integer,NULL::text)",
				"SELECT photo.semantic_detector_preparation_registration_state(NULL::uuid,NULL::uuid,NULL::uuid,NULL::uuid,NULL::text,NULL::text,NULL::jsonb,NULL::jsonb,NULL::jsonb,NULL::jsonb,NULL::jsonb,NULL::integer,NULL::text)",
				"SELECT photo.semantic_person_preparation_registration_state(NULL::uuid,NULL::uuid,NULL::uuid,NULL::uuid,NULL::text,NULL::text,NULL::jsonb,NULL::jsonb,NULL::jsonb,NULL::jsonb,NULL::jsonb,NULL::integer,NULL::text)",
			} {
				for _, role := range []string{"photo_api", "photo_worker", "photo_publisher", "photo_portal", "photo_auth", "photo_gateway", "photo_maintenance", "photo_semantic_admission"} {
					roleTx, err := evidenceOwner.Begin(ctx)
					if err != nil {
						t.Fatal(err)
					}
					if _, err = roleTx.Exec(ctx, "SET LOCAL ROLE "+pgx.Identifier{role}.Sanitize()); err != nil {
						roleTx.Rollback(context.Background())
						t.Fatal(err)
					}
					_, err = roleTx.Exec(ctx, gate)
					roleTx.Rollback(context.Background())
					var denied *pgconn.PgError
					if !errors.As(err, &denied) || denied.Code != "42501" {
						t.Fatal("defect execution gate role not denied", role, gate, err)
					}
				}
			}
			t.Log("V27_DEFECT_EXECUTION_LOCK_ACL_OK")
			t.Log("V27_DEFECT_NATIVE_PARAMS_REGISTRATION_ACL_OK")

			coverageLockPlan := WorkflowPlan{tenant: fixture.Tenant, actor: who.UserID, project: reader.ProjectID, run: request.RunID, tasks: []v2input.DraftTask{{}}, person: []personProtectionProof{retainedCoverageProof, retainedCoverageProof}}
			coverageLockTx, err := manualLockPool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = coverageLockTx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", fixture.Tenant.String()); err != nil {
				t.Fatal(err)
			}
			if err := LockPersonProducerInTransaction(ctx, coverageLockTx, who, request, coverageLockPlan); err != nil {
				coverageLockTx.Rollback(context.Background())
				t.Fatal("person retained protection locks", err)
			}
			coverageWriter, err := evidenceOwner.Acquire(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = coverageWriter.Exec(ctx, "SET lock_timeout='250ms'"); err != nil {
				t.Fatal(err)
			}
			for _, id := range []uuid.UUID{retainedProtectionProof.root.ref.ArtifactID, retainedProtectionProof.pixels.ref.ArtifactID, fixture.PersonReport.ArtifactID} {
				_, err := coverageWriter.Exec(ctx, "UPDATE photo.artifacts SET expires_at=expires_at WHERE id=$1", id)
				var denied *pgconn.PgError
				if !errors.As(err, &denied) || denied.Code != "55P03" {
					t.Fatal("person/protection evidence write crossed locks", err)
				}
			}
			coverageWriter.Exec(context.Background(), "RESET lock_timeout")
			coverageWriter.Release()
			coverageLockTx.Rollback(context.Background())
			t.Log("V27_PERSON_RETAINED_PROTECTION_LOCK_OK")
			lockProtectionEvidence := func(proof protectionPreparationProof) error {
				tx, err := manualLockPool.Begin(ctx)
				if err != nil {
					return err
				}
				defer tx.Rollback(context.Background())
				if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", fixture.Tenant.String()); err != nil {
					return err
				}
				if err = reader.lockProtectionPreparationProof(ctx, tx, who, fixture.Tenant, request.Intent.Reference, proof); err != nil {
					return err
				}
				writer, err := evidenceOwner.Acquire(ctx)
				if err != nil {
					return err
				}
				defer writer.Release()
				if _, err = writer.Exec(ctx, "SET lock_timeout='250ms'"); err != nil {
					return err
				}
				defer writer.Exec(context.Background(), "RESET lock_timeout")
				for _, id := range []uuid.UUID{proof.root.ref.ArtifactID, proof.pixels.ref.ArtifactID} {
					_, err := writer.Exec(ctx, "UPDATE photo.artifacts SET expires_at=expires_at WHERE id=$1", id)
					var denied *pgconn.PgError
					if !errors.As(err, &denied) || denied.Code != "55P03" {
						t.Fatal("protection pin write not blocked", err)
					}
				}
				return nil
			}
			if err := lockProtectionEvidence(retainedProtectionProof); err != nil {
				t.Fatal("protection evidence locks", err)
			}
			changedProtectionProof := retainedProtectionProof
			changedProtectionProof.pixels.object.Key += "-changed"
			if err := lockProtectionEvidence(changedProtectionProof); !errors.Is(err, v2input.ErrConflict) {
				t.Fatal("changed protection pixels locked", err)
			}
			t.Log("V27_PROTECTION_EVIDENCE_LOCK_OK")
			protectionBatchTx, err := manualLockPool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = protectionBatchTx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", fixture.Tenant.String()); err != nil {
				t.Fatal(err)
			}
			if err := reader.lockProtectionPreparationProofs(ctx, protectionBatchTx, who, fixture.Tenant, request.Intent.Reference, []protectionPreparationProof{retainedProtectionProof, retainedProtectionProof}); err != nil {
				protectionBatchTx.Rollback(context.Background())
				t.Fatal("protection batch barrier", err)
			}
			declarationWriter, err := evidenceOwner.Acquire(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = declarationWriter.Exec(ctx, "SET lock_timeout='250ms'"); err != nil {
				t.Fatal(err)
			}
			_, declarationErr := declarationWriter.Exec(ctx, "UPDATE photo.interactive_tasks SET source_artifact_id=source_artifact_id WHERE task_id=(SELECT created_by_task_id FROM photo.artifacts WHERE id=$1)", retainedProtectionProof.root.ref.ArtifactID)
			var declarationDenied *pgconn.PgError
			declarationWriter.Exec(context.Background(), "RESET lock_timeout")
			declarationWriter.Release()
			protectionBatchTx.Rollback(context.Background())
			if !errors.As(declarationErr, &declarationDenied) || declarationDenied.Code != "55P03" {
				t.Fatal("protection input declaration crossed barrier", declarationErr)
			}
			t.Log("V27_PROTECTION_BATCH_DECLARATION_LOCK_OK")

			executionSourceTx, err := manualLockPool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if err = reader.lockDefectExecutionSourceInTransaction(ctx, executionSourceTx, who, fixture.Tenant, request.Intent.Reference, preparedSourceExecution); err != nil {
				executionSourceTx.Rollback(context.Background())
				t.Fatal("retained execution source transaction", err)
			}

			executionWriter, err := evidenceOwner.Acquire(ctx)
			if err != nil {
				executionSourceTx.Rollback(context.Background())
				t.Fatal(err)
			}
			if _, err = executionWriter.Exec(ctx, "SET lock_timeout='250ms'"); err != nil {
				executionWriter.Release()
				executionSourceTx.Rollback(context.Background())
				t.Fatal(err)
			}
			for _, artifact := range []uuid.UUID{preparedSource.Inputs.Pixels.ArtifactID, preparedSourceExecution.qualification.report.ref.ArtifactID} {
				_, writeErr := executionWriter.Exec(ctx, "UPDATE photo.artifacts SET expires_at=expires_at WHERE id=$1", artifact)
				var denied *pgconn.PgError
				if !errors.As(writeErr, &denied) || denied.Code != "55P03" {
					executionWriter.Exec(context.Background(), "RESET lock_timeout")
					executionWriter.Release()
					executionSourceTx.Rollback(context.Background())
					t.Fatal("retained execution lock allowed concurrent artifact mutation", writeErr)
				}
			}
			executionSourceTx.Rollback(context.Background())
			for _, artifact := range []uuid.UUID{preparedSource.Inputs.Pixels.ArtifactID, preparedSourceExecution.qualification.report.ref.ArtifactID} {
				if _, err = executionWriter.Exec(ctx, "UPDATE photo.artifacts SET expires_at=expires_at WHERE id=$1", artifact); err != nil {
					executionWriter.Exec(context.Background(), "RESET lock_timeout")
					executionWriter.Release()
					t.Fatal("execution rollback did not release artifact lock", err)
				}
			}
			executionWriter.Exec(context.Background(), "RESET lock_timeout")
			executionWriter.Release()
			t.Log("V27_DEFECT_EXECUTION_SOURCE_TRANSACTION_OK")

			faceExecutionTx, err := manualLockPool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if err = reader.lockDefectExecutionFacesInTransaction(ctx, faceExecutionTx, who, fixture.Tenant, request.Intent.Reference, preparedFaceExecution); err != nil {
				faceExecutionTx.Rollback(context.Background())
				t.Fatal("retained face execution transaction", err)
			}
			faceExecutionTx.Rollback(context.Background())
			t.Log("V27_DEFECT_FACE_EXECUTION_TRANSACTION_OK")
			parserExecutionTx, err := manualLockPool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if err = reader.lockDefectExecutionParsersInTransaction(ctx, parserExecutionTx, who, fixture.Tenant, request.Intent.Reference, preparedMaskExecution); err != nil {
				parserExecutionTx.Rollback(context.Background())
				t.Fatal("retained parser execution transaction", err)
			}
			parserExecutionTx.Rollback(context.Background())
			t.Log("V27_DEFECT_PARSER_EXECUTION_TRANSACTION_OK")
			if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=NULL WHERE id=$1", fixture.ManualFaceDefectPins.Report.ArtifactID); err != nil {
				t.Fatal(err)
			}
			manualProfileParams := PublishedDefectParams{Ref: fixture.ManualFaceDefectPins.Params, Inputs: append([]review.ArtifactRef(nil), fullSourceParams.Inputs...)}
			manualProfileNodes := append([]v2draft.LayerRef(nil), fullSourceNodes...)
			manualProfileNodes[0].Ref = manualProfileParams.Ref
			manualTarget := v2input.Ref{ArtifactID: manualRoots[0].Binding.Mask.Artifact.ID, SHA256: manualRoots[0].Binding.Mask.Artifact.SHA256}
			for _, node := range []v2draft.LayerRef{
				{Ref: fixture.DefectLandmark, Contract: "FaceLandmarks", CanvasHash: request.Intent.Reference.CanvasHash},
				{Ref: fixture.ManualFaceMaskRoot, Contract: "MaskRef", CanvasHash: request.Intent.Reference.CanvasHash},
				{Ref: manualTarget, Contract: "MaskPixels", ParentRef: fixture.ManualFaceMaskRoot},
				{Ref: retainedManualProof.children[0].ref, Contract: "MaskPixels", ParentRef: fixture.ManualFaceMaskRoot},
			} {
				manualProfileParams.Inputs = append(manualProfileParams.Inputs, review.ArtifactRef{ID: node.Ref.ArtifactID, SHA256: node.Ref.SHA256})
				manualProfileNodes = append(manualProfileNodes, node)
			}
			serverReports := nativeDefectQualificationReports{Defect: defectQ.Report, Faces: faceQualificationReports, Parser: skinProfileReports}
			changedReports := serverReports
			changedReports.Parser[1].ArtifactID = uuid.New()
			deniedExecution, deniedErr := reader.prepareSelectedDefectExecution(ctx, who, fixture.Tenant, request.Intent.Reference, color, fixture.ManualFaceDefectPins, changedReports, manualProfileParams, manualProfileNodes, defectCosts, 1<<40, 0, 1<<40, 1<<40)
			if !errors.Is(deniedErr, v2input.ErrConflict) || deniedExecution.policy != (qualificationPolicySnapshot{}) {
				t.Fatal("server policy selection accepted substituted detection report", deniedErr)
			}
			preparedPolicyExecution, err := reader.prepareDiscoveredDefectExecution(ctx, who, fixture.Tenant, request.Intent.Reference, color, fixture.ManualFaceDefectPins, manualProfileParams, manualProfileNodes, defectCosts, 1<<40, 0, 1<<40, 1<<40)
			preparedManualExecution := preparedPolicyExecution.execution
			if err != nil || preparedManualExecution.manualCount != 1 || preparedManualExecution.parserCount != 0 {
				t.Fatal("original manual execution preparation", err)
			}
			selectedReports, err := selectQualificationPolicies(preparedPolicyExecution.policy, []string{"face.landmarks", "face.detect", "face.parse", "defects.propose"})
			if err != nil {
				t.Fatal(err)
			}
			for _, operation := range []string{"face.landmarks", "face.detect", "face.parse", "defects.propose"} {
				expected := selectedReports[operation]
				ref, err := reader.discoverQualificationReport(ctx, who, fixture.Tenant, expected)
				if err != nil {
					t.Fatal("report discovery", operation, err)
				}
				if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE tenant_id=$1 AND id=$2", fixture.Tenant, ref.ArtifactID); err != nil {
					t.Fatal(err)
				}
				denied, err := reader.discoverQualificationReport(ctx, who, fixture.Tenant, expected)
				if err == nil || denied != (v2input.Ref{}) {
					t.Fatal("expired report discovered", operation, denied, err)
				}
				if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=NULL WHERE tenant_id=$1 AND id=$2", fixture.Tenant, ref.ArtifactID); err != nil {
					t.Fatal(err)
				}
				restored, err := reader.discoverQualificationReport(ctx, who, fixture.Tenant, expected)
				if err != nil || restored != ref {
					t.Fatal("restored report not discovered", operation, restored, err)
				}
			}
			for _, role := range []string{"photo_api", "photo_scheduler", "photo_worker", "photo_publisher", "photo_portal", "photo_auth", "photo_gateway", "photo_maintenance", "photo_semantic_admission"} {
				roleTx, err := evidenceOwner.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := roleTx.Exec(ctx, "SET LOCAL ROLE "+pgx.Identifier{role}.Sanitize()); err != nil {
					roleTx.Rollback(context.Background())
					t.Fatal(err)
				}
				_, callErr := roleTx.Exec(ctx, "SELECT * FROM photo.semantic_proposal_defect_qualification_candidates(NULL::uuid,NULL::uuid,NULL::uuid,NULL::uuid,NULL::jsonb)")
				roleTx.Rollback(context.Background())
				if role == "photo_api" {
					if callErr != nil {
						t.Fatal("API discovery EXECUTE unavailable", callErr)
					}
				} else {
					var denied *pgconn.PgError
					if !errors.As(callErr, &denied) || denied.Code != "42501" {
						t.Fatal("discovery SQL role bypass", role, callErr)
					}
				}
			}
			t.Log("V27_DEFECT_DISCOVERY_SQL_ACL_OK")
			t.Log("V27_DEFECT_REPORT_DISCOVERY_OK")
			t.Log("V27_DEFECT_SERVER_POLICY_SELECTION_OK")
			originalParamsPin := preparedManualExecution.face.originProof.objects[0]
			originalParamsBody, err := v2input.ReadPinnedSemanticDocument(ctx, objects, originalParamsPin.object, originalParamsPin.ref)
			if err != nil {
				t.Fatal(err)
			}
			retainedParams, err := reader.prepareOriginalDefectParams(ctx, who, fixture.Tenant, request.Intent.Reference, color, preparedPolicyExecution)
			if err != nil || retainedParams.manualCount != 1 || retainedParams.params.params != string(originalParamsBody) || retainedParams.params.digest != fixture.ManualFaceDefectPins.Params.SHA256 {
				t.Fatal("retained original manual Params", err)
			}
			if err := reader.recheckManualDefectParams(ctx, who, fixture.Tenant, request.Intent.Reference, color, retainedParams); err != nil {
				t.Fatal("manual Params original closure recheck", err)
			}
			lostParams := retainedParams
			lostParams.manualCount = 0
			if err := reader.recheckManualDefectParams(ctx, who, fixture.Tenant, request.Intent.Reference, color, lostParams); !errors.Is(err, v2input.ErrInvalid) {
				t.Fatal("manual Params missing proof accepted", err)
			}
			t.Log("V27_DEFECT_MANUAL_PARAMS_RETAINED_OK")
			t.Log("V27_DEFECT_MANUAL_PARAMS_RECHECK_OK")
			var nativeParamsRun uuid.UUID
			if err := evidenceOwner.QueryRow(ctx, "SELECT run_id FROM photo.tasks WHERE id=(SELECT created_by_task_id FROM photo.artifacts WHERE tenant_id=$1 AND id=$2) AND tenant_id=$1", fixture.Tenant, fixture.ManualFaceDefectPins.Report.ArtifactID).Scan(&nativeParamsRun); err != nil {
				t.Fatal(err)
			}
			nativeParamsScope := artifactjson.Scope{Tenant: fixture.Tenant, Run: nativeParamsRun, Owner: uuid.New(), Purpose: "task_params"}
			nativePutCalled := false
			nativePublisher := artifactjson.Publisher{Repository: artifactjson.SQLRepository{DB: manualLockPool, Bucket: fixture.Objects.Bucket}, Objects: afterIntentPut{ImmutableStore: objects, after: func() error { nativePutCalled = true; return nil }}}
			deniedNativeParams, nativeErr := reader.publishManualDefectParams(ctx, who, fixture.Tenant, request.Intent.Reference, color, nativePublisher, nativeParamsScope, retainedParams)
			if !errors.Is(nativeErr, artifactjson.ErrStale) || nativePutCalled || deniedNativeParams.Ref.ArtifactID != uuid.Nil || deniedNativeParams.Inputs != nil {
				t.Fatal("native manual Params bypassed graph owner", nativeErr)
			}
			var nativeReservations int
			if err := evidenceOwner.QueryRow(ctx, "SELECT count(*) FROM photo.backend_documents WHERE tenant_id=$1 AND run_id=$2 AND owner_id=$3", fixture.Tenant, nativeParamsRun, nativeParamsScope.Owner).Scan(&nativeReservations); err != nil || nativeReservations != 0 {
				t.Fatal("native forbidden Params reserved document", err)
			}
			t.Log("V27_DEFECT_NATIVE_PARAMS_OWNER_DENIED_OK")
			versionParamsID := uuid.NewSHA1(request.RunID, []byte("semantic.defect.params:reference"))
			versionParams, err := reader.publishManualDefectVersionParams(ctx, who, fixture.Tenant, request.Intent.Reference, color, publisher, versionParamsID, retainedParams)
			if err != nil || versionParams.Ref.SHA256 != retainedParams.params.digest || len(versionParams.ManualRoots) != 1 {
				t.Fatal("native accepted-version Params publication", err)
			}
			versionReplay, err := reader.publishManualDefectVersionParams(ctx, who, fixture.Tenant, request.Intent.Reference, color, publisher, versionParamsID, retainedParams)
			if err != nil || versionReplay.Ref != versionParams.Ref {
				t.Fatal("native accepted-version Params replay", err)
			}
			t.Log("V27_DEFECT_VERSION_PARAMS_PUBLICATION_OK")
			versionParamsProof, err := reader.readDefectVersionParamsProof(ctx, who, fixture.Tenant, request.Intent.Reference, color, versionParamsID, versionParams, retainedParams)
			if err != nil {
				t.Fatal("native published Params original proof", err)
			}
			versionParamsTx, err := manualLockPool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if err := reader.lockDefectVersionParamsProof(ctx, versionParamsTx, who, fixture.Tenant, request.Intent.Reference, versionParamsProof); err != nil {
				versionParamsTx.Rollback(context.Background())
				t.Fatal("native published Params transaction pins", err)
			}
			if err := reader.lockPublishedDefectExecutionInTransaction(ctx, versionParamsTx, who, fixture.Tenant, request.Intent.Reference, preparedPolicyExecution, versionParamsProof); err != nil {
				versionParamsTx.Rollback(context.Background())
				t.Fatal("new Params qualified execution locks", err)
			}
			versionParamsTx.Rollback(context.Background())
			t.Log("V27_DEFECT_VERSION_PARAMS_PROOF_LOCK_OK")
			t.Log("V27_DEFECT_PUBLISHED_EXECUTION_LOCKS_OK")
			originalExecutionFiles, err := reader.decodeDefectExecutionFilePins(ctx, who, fixture.Tenant, request.Intent.Reference, preparedManualExecution.face.source)
			if err != nil {
				t.Fatal(err)
			}
			var taskNativeRefs []review.ArtifactRef
			if err := json.Unmarshal([]byte(retainedParams.params.inputs), &taskNativeRefs); err != nil {
				t.Fatal(err)
			}
			taskEntries := []v2input.Entry{{Ref: versionParams.Ref, Contract: "Params"}}
			taskSizes := []v2input.SemanticInputSize{{Ref: versionParams.Ref, Bytes: versionParamsProof.object.Bytes}}
			for _, ref := range taskNativeRefs {
				found := false
				for _, file := range originalExecutionFiles {
					if file.Ref.ArtifactID != ref.ID || file.Ref.SHA256 != ref.SHA256 {
						continue
					}
					contract := file.Contract
					switch contract {
					case "WorkingPixels", "WorkingAlpha", "MaskPixels", "DenseWarp":
						contract = "NpyArray"
					}
					taskEntries = append(taskEntries, v2input.Entry{Ref: file.Ref, Contract: contract})
					taskSizes = append(taskSizes, v2input.SemanticInputSize{Ref: file.Ref, Bytes: file.Object.Bytes})
					found = true
					break
				}
				if !found {
					t.Fatal("native defect task missing original file metadata")
				}
			}
			taskImageBody, err := reader.Read(ctx, who, fixture.Tenant, request.Intent.Reference)
			if err != nil {
				t.Fatal(err)
			}
			frozenDefectTask, err := v2input.CompileSemanticDefectTask(ctx, request.WorkflowID, 0, request.Intent.Reference, taskImageBody, []byte(retainedParams.params.params), versionParams.Ref, taskEntries, taskSizes, defectCosts, 1<<40, 0, 1<<40, 1<<40)
			if err != nil {
				t.Fatal("native published defect task compilation", err)
			}
			defectDraft, err := frozenDefectTask.DraftTask()
			if err != nil || defectDraft.Operation != "defects.propose" || defectDraft.ParamsArtifactID != versionParams.Ref.ArtifactID || defectDraft.ResourceClass != "cpu" {
				t.Fatal("native published defect task draft", err)
			}
			t.Log("V27_DEFECT_PUBLISHED_TASK_COMPILE_OK")
			defectStagePlan, err := reader.prepareFirstDefectPlan(ctx, who, request, "proposal-publication", first, snapshotRef, snapshotBody, preparedPolicyExecution, color, publisher, defectCosts, 1<<40, 0, 1<<40, 1<<40, 64<<20)
			if err != nil {
				t.Fatal("native defect preparation draft plan", err)
			}
			firstPlanReplay, err := reader.prepareFirstDefectPlan(ctx, who, request, "proposal-publication", first, snapshotRef, snapshotBody, preparedPolicyExecution, color, publisher, defectCosts, 1<<40, 0, 1<<40, 1<<40, 64<<20)
			if err != nil {
				t.Fatal("first native plan reconstruction", err)
			}
			originalShell, err := canonical.Marshal(defectStagePlan.shell)
			if err != nil {
				t.Fatal(err)
			}
			retainedShell, err := canonical.Marshal(firstPlanReplay.shell)
			if err != nil || string(originalShell) != string(retainedShell) || firstPlanReplay.params.params != versionParams.Ref || firstPlanReplay.params.document != versionParamsID {
				t.Fatal("first native plan changed on reconstruction", err)
			}
			t.Log("V27_DEFECT_FIRST_PLAN_RECONSTRUCTION_OK")
			sourceAuthorityBody, err := reader.marshalDefectSourceAuthority(ctx, who, fixture.Tenant, request.Intent.Reference, preparedPolicyExecution)
			if err != nil {
				t.Fatal("native source authority packet", err)
			}
			expectedPolicyBody, err := canonical.Bytes([]byte(preparedPolicyExecution.policy.body))
			if err != nil {
				t.Fatal(err)
			}
			var sourceAuthority defectSourceAuthority
			if json.Unmarshal(sourceAuthorityBody, &sourceAuthority) != nil || sourceAuthority.SchemaVersion != 1 || sourceAuthority.Binding != request.Intent.Reference || string(sourceAuthority.Policy) != string(expectedPolicyBody) {
				t.Fatal("source authority roundtrip changed scope or policy")
			}
			faceAuthorityBody, err := reader.marshalDefectFaceAuthority(ctx, who, fixture.Tenant, request.Intent.Reference, preparedPolicyExecution.execution.face)
			if err != nil {
				t.Fatal("native face authority packet", err)
			}
			var faceAuthority []defectFaceAuthorityEntry
			if json.Unmarshal(faceAuthorityBody, &faceAuthority) != nil || len(faceAuthority) != preparedPolicyExecution.execution.face.producerCount {
				t.Fatal("face authority roundtrip changed producer count")
			}
			parserAuthorityBody, err := reader.marshalDefectParserAuthority(ctx, who, fixture.Tenant, request.Intent.Reference, preparedPolicyExecution.execution)
			if err != nil {
				t.Fatal("native parser authority packet", err)
			}
			var parserAuthority defectParserAuthority
			if json.Unmarshal(parserAuthorityBody, &parserAuthority) != nil || parserAuthority.SchemaVersion != 1 || len(parserAuthority.Parsers) != preparedPolicyExecution.execution.parserCount || string(parserAuthority.Roots) != preparedPolicyExecution.execution.roots {
				t.Fatal("parser authority roundtrip changed roots or count")
			}
			foreignAuthorityBinding := request.Intent.Reference
			foreignAuthorityBinding.AssetVersionID = uuid.New()
			if body, err := reader.marshalDefectSourceAuthority(ctx, who, fixture.Tenant, foreignAuthorityBinding, preparedPolicyExecution); err == nil || body != nil {
				t.Fatal("foreign source authority binding accepted", err)
			}
			if body, err := reader.marshalDefectFaceAuthority(ctx, who, fixture.Tenant, foreignAuthorityBinding, preparedPolicyExecution.execution.face); err == nil || body != nil {
				t.Fatal("foreign face authority binding accepted", err)
			}
			if body, err := reader.marshalDefectParserAuthority(ctx, who, fixture.Tenant, foreignAuthorityBinding, preparedPolicyExecution.execution); err == nil || body != nil {
				t.Fatal("foreign parser authority binding accepted", err)
			}
			t.Log("V27_DEFECT_AUTHORITY_PACKETS_ROUNDTRIP_OK")
			defectStageTx, err := evidenceOwner.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := defectStageTx.Exec(ctx, "SET LOCAL ROLE photo_scheduler"); err != nil {
				defectStageTx.Rollback(context.Background())
				t.Fatal(err)
			}
			defectStageAdmission, err := reader.beginDefectPreparationDraftInTransaction(ctx, defectStageTx, who, defectStagePlan)
			if err != nil || defectStageAdmission.Outcome != "created" {
				defectStageTx.Rollback(context.Background())
				t.Fatal("native defect blocked draft creation", err)
			}
			defectStageReplay, err := reader.beginDefectPreparationDraftInTransaction(ctx, defectStageTx, who, defectStagePlan)
			if err != nil || defectStageReplay.Outcome != "pending" || defectStageReplay.RunID != defectStageAdmission.RunID {
				defectStageTx.Rollback(context.Background())
				t.Fatal("native defect blocked draft replay", err)
			}
			// Test-owner visibility only: inspect the uncommitted draft without new
			// production grants. Creation/replay above ran as photo_scheduler.
			if _, err := defectStageTx.Exec(ctx, "SET LOCAL ROLE NONE"); err != nil {
				defectStageTx.Rollback(context.Background())
				t.Fatal(err)
			}
			var stageStatus, stageTaskState string
			var stageSealed bool
			if err := defectStageTx.QueryRow(ctx, "SELECT r.status,r.graph_sealed,t.state FROM photo.runs r JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id WHERE r.tenant_id=$1 AND r.id=$2", fixture.Tenant, defectStageAdmission.RunID).Scan(&stageStatus, &stageSealed, &stageTaskState); err != nil || stageStatus != "draft" || stageSealed || stageTaskState != "blocked" {
				defectStageTx.Rollback(context.Background())
				t.Fatal("native defect stage activated early", err)
			}
			defectStageTx.Rollback(context.Background())
			t.Log("V27_DEFECT_BLOCKED_DRAFT_CREATE_REPLAY_OK")
			defectTransportPublisher := v2input.ScopedPublisher{Documents: artifactjson.Publisher{Repository: artifactjson.SQLRepository{DB: manualLockPool, Bucket: fixture.Objects.Bucket}, Objects: objects}, Tenant: fixture.Tenant, Run: defectStagePlan.shell.Run}
			defectTransport, err := reader.persistDefectPreparationInputs(ctx, manualLockPool, defectTransportPublisher, who, defectStagePlan)
			if err != nil {
				t.Fatal("native defect physical transport", err)
			}
			defectRegistrationTx, err := manualLockPool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			for attempt := 0; attempt < 2; attempt++ {
				if err := reader.registerDefectPreparationInputsInTransaction(ctx, defectRegistrationTx, who, defectStagePlan, defectTransport.Graph.Built); err != nil {
					defectRegistrationTx.Rollback(context.Background())
					t.Fatal("native defect input registration/replay", attempt, err)
				}
			}
			if err := defectRegistrationTx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			t.Log("V27_DEFECT_INPUT_REGISTRATION_REPLAY_OK")
			defectResumePool, err := pgxpool.NewWithConfig(ctx, manualLockConfig.Copy())
			if err != nil {
				t.Fatal(err)
			}
			defer defectResumePool.Close()
			defectResumeTx, err := defectResumePool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if err := reader.registerDefectPreparationInputsInTransaction(ctx, defectResumeTx, who, defectStagePlan, defectTransport.Graph.Built); err != nil {
				defectResumeTx.Rollback(context.Background())
				t.Fatal("native defect durable registration replay", err)
			}
			if err := defectResumeTx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			t.Log("V27_DEFECT_INPUT_DURABLE_REPLAY_OK")
			defectWrongTransport := defectTransport.Graph.Built
			defectWrongTransport.Manifest.ArtifactID = uuid.New()
			defectWrongTx, err := defectResumePool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			wrongTransportErr := reader.registerDefectPreparationInputsInTransaction(ctx, defectWrongTx, who, defectStagePlan, defectWrongTransport)
			defectWrongTx.Rollback(context.Background())
			if !errors.Is(wrongTransportErr, v2input.ErrConflict) {
				t.Fatal("native defect substituted manifest accepted", wrongTransportErr)
			}
			var durableStatus, durableTaskState string
			var durableSealed bool
			if err := evidenceOwner.QueryRow(ctx, "SELECT r.status,r.graph_sealed,t.state FROM photo.runs r JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id WHERE r.tenant_id=$1 AND r.id=$2", fixture.Tenant, defectStagePlan.shell.Run).Scan(&durableStatus, &durableSealed, &durableTaskState); err != nil || durableStatus != "draft" || durableSealed || durableTaskState != "blocked" {
				t.Fatal("registered native defect activated early", err)
			}
			t.Log("V27_DEFECT_INPUT_MANIFEST_SUBSTITUTION_DENIED_OK")
			coordinatorReplay, err := reader.persistDefectPreparationInputs(ctx, defectResumePool, defectTransportPublisher, who, defectStagePlan)
			if err != nil || coordinatorReplay.Graph.Built.Manifest != defectTransport.Graph.Built.Manifest || coordinatorReplay.InputDigest != defectTransport.InputDigest {
				t.Fatal("native defect coordinator replay", err)
			}
			t.Log("V27_DEFECT_PERSISTENCE_COORDINATOR_OK")
			defectPersonAdmission, err := reader.prepareDefectPersonAdmission(ctx, who, defectStagePlan, retainedCoverageProof)
			if err != nil {
				t.Fatal("native defect person admission preparation", err)
			}
			admissionEvidenceBody, err := reader.marshalDefectAdmissionEvidence(ctx, who, defectPersonAdmission, defectTransport.Graph.Built)
			if err != nil {
				t.Fatal("unified defect admission packet", err)
			}
			var admissionEvidence defectAdmissionEvidence
			if json.Unmarshal(admissionEvidenceBody, &admissionEvidence) != nil || admissionEvidence.SchemaVersion != 1 || admissionEvidence.Tenant != fixture.Tenant || admissionEvidence.Project != reader.ProjectID || admissionEvidence.Actor != who.UserID || admissionEvidence.Session != who.SessionID || admissionEvidence.Run != defectStagePlan.shell.Run || admissionEvidence.ParentRun != request.RunID || admissionEvidence.ParamsDocument != versionParamsProof.document || admissionEvidence.RequestSHA != request.RequestSHA {
				t.Fatal("unified packet changed scope/task/document")
			}
			var packetRegistration defectRegistrationBinding
			var packetContent defectActivationContent
			var packetPerson defectPersonAuthority
			if json.Unmarshal(admissionEvidence.Registration, &packetRegistration) != nil || json.Unmarshal(admissionEvidence.Content, &packetContent) != nil || json.Unmarshal(admissionEvidence.Person, &packetPerson) != nil || packetRegistration.Params != packetContent.Params || packetRegistration.Params != versionParamsProof.params || packetRegistration.TaskID != defectStagePlan.shell.Tasks[0].ID || packetPerson.Protection != retainedCoverageProof.Protection || packetPerson.ProtectedPixels != retainedCoverageProof.protectionEvidence.pixels.ref {
				t.Fatal("unified packet lost cross-bindings")
			}
			for changed := 0; changed < 5; changed++ {
				candidate := defectPersonAdmission
				switch changed {
				case 0:
					candidate.plan.shell.Run = uuid.New()
				case 1:
					candidate.plan.shell.Tasks = append([]v2input.DraftTask(nil), candidate.plan.shell.Tasks...)
					candidate.plan.shell.Tasks[0].ParamsSHA = strings.Repeat("0", 64)
				case 2:
					candidate.plan.params.params.ArtifactID = uuid.New()
				case 3:
					candidate.plan.params.object.Bytes++
				case 4:
					candidate.plan.request.RequestSHA = "invalid"
				}
				if body, err := reader.marshalDefectAdmissionEvidence(ctx, who, candidate, defectTransport.Graph.Built); err == nil || body != nil {
					t.Fatal("incoherent unified packet accepted", changed, err)
				}
			}
			t.Log("V27_DEFECT_UNIFIED_ADMISSION_PACKET_OK")
			wrongPerson := retainedCoverageProof
			wrongPerson.Binding.AssetVersionID = uuid.New()
			if err := reader.validateDefectPersonAdmission(ctx, who, defectStagePlan, wrongPerson); !errors.Is(err, v2input.ErrInvalid) {
				t.Fatal("native defect foreign person proof accepted", err)
			}
			defectPersonTx, err := defectResumePool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer defectPersonTx.Rollback(context.Background())
			if err := reader.lockRegisteredDefectPersonAdmission(ctx, defectPersonTx, who, defectPersonAdmission, defectTransport.Graph.Built); err != nil {
				defectPersonTx.Rollback(context.Background())
				t.Fatal("native defect person admission locks", err)
			}
			// The native stage above is manual-only. Verify the skin component using
			// the separately retained, real skin/parser preparation from this fixture.
			skinEvidence := admissionEvidence
			skinEvidence.Parser, err = reader.marshalDefectParserAuthority(ctx, who, fixture.Tenant, request.Intent.Reference, preparedMaskExecution)
			if err != nil {
				t.Fatal("skin parser authority", err)
			}
			skinOrigin := fixture.SkinFaceDefectPins
			skinEvidence.OriginRefs, err = json.Marshal([]v2input.Ref{skinOrigin.Proposal, skinOrigin.Report, skinOrigin.Labels, skinOrigin.Protection, skinOrigin.Params})
			if err != nil {
				t.Fatal(err)
			}
			// The same transaction already owns tenant/run locks. A second owner
			// transaction would block on those locks; change only its local role.
			if _, err := defectPersonTx.Exec(ctx, "SET LOCAL ROLE photo_fn_owner"); err != nil {
				t.Fatal(err)
			}
			for changed := 0; changed < 4; changed++ {
				candidate := skinEvidence
				var parser defectParserAuthority
				if err := json.Unmarshal(candidate.Parser, &parser); err != nil {
					t.Fatal(err)
				}
				var roots []review.ProposalFaceMaskRoot
				if err := json.Unmarshal(parser.Roots, &roots); err != nil || len(roots) != 1 || len(parser.Parsers) != 1 {
					t.Fatal("skin lineage fixture", err)
				}
				switch changed {
				case 1:
					roots[0].Root.ID = uuid.New()
				case 2:
					parser.Parsers[0].Pixels.ArtifactID = uuid.New()
				case 3:
					parser.Parsers = append(parser.Parsers, parser.Parsers[0])
				}
				parser.Roots, err = json.Marshal(roots)
				if err != nil {
					t.Fatal(err)
				}
				candidate.Parser, err = json.Marshal(parser)
				if err != nil {
					t.Fatal(err)
				}
				body, err := json.Marshal(candidate)
				if err != nil {
					t.Fatal(err)
				}
				var accepted bool
				if err := defectPersonTx.QueryRow(ctx, "SELECT photo.lock_semantic_defect_skin_lineage($1,$2::jsonb)", fixture.Tenant, string(body)).Scan(&accepted); err != nil || accepted != (changed == 0) {
					t.Fatal("skin lineage component", changed, accepted, err)
				}
			}
			for changed := 0; changed < 5; changed++ {
				var candidate map[string]any
				if err := json.Unmarshal(admissionEvidenceBody, &candidate); err != nil {
					t.Fatal(err)
				}
				authority := candidate["source_authority"].(map[string]any)
				expected := authority["expected"].(map[string]any)
				switch changed {
				case 1:
					expected["TestFixtureSHAs"] = map[string]any{strings.Repeat("0", 64): map[string]any{}}
				case 2:
					expected["EvidenceSHAs"] = map[string]any{}
				case 3:
					expected["CalibrationSHAs"] = map[string]any{strings.Repeat("0", 64): map[string]any{}}
				case 4:
					policies := authority["policy"].([]any)
					for _, policy := range policies {
						if policy.(map[string]any)["operation"] == "defects.propose" {
							authority["policy"] = append(policies, policy)
							break
						}
					}
				}
				body, err := json.Marshal(candidate)
				if err != nil {
					t.Fatal(err)
				}
				var matched bool
				if err := defectPersonTx.QueryRow(ctx, "SELECT photo.semantic_defect_policy_expectations_match($1::jsonb)", string(body)).Scan(&matched); err != nil || matched != (changed == 0) {
					t.Fatal("complete policy expectation match", changed, matched, err)
				}
			}
			t.Log("V27_DEFECT_SQL_POLICY_EXPECTATIONS_OK")
			for changed := 0; changed < 4; changed++ {
				candidate := admissionEvidence
				switch changed {
				case 1:
					candidate.Anchor.IntentText += " "
				case 2:
					candidate.Anchor.SnapshotText += " "
				case 3:
					candidate.RequestSHA = strings.Repeat("0", 64)
				}
				body, err := json.Marshal(candidate)
				if err != nil {
					t.Fatal(err)
				}
				var matched bool
				if err := defectPersonTx.QueryRow(ctx, "SELECT photo.lock_semantic_defect_admission_anchor($1,$2,$3::jsonb)", fixture.Tenant, defectStagePlan.shell.Run, string(body)).Scan(&matched); err != nil || matched != (changed == 0) {
					t.Fatal("native intent snapshot anchor", changed, matched, err)
				}
			}
			t.Log("V27_DEFECT_SQL_INTENT_SNAPSHOT_ANCHOR_OK")
			for changed := 0; changed < 4; changed++ {
				if _, err := defectPersonTx.Exec(ctx, "SAVEPOINT native_budget_case"); err != nil {
					t.Fatal(err)
				}
				switch changed {
				case 1:
					_, err = defectPersonTx.Exec(ctx, "UPDATE photo.quota_reservations SET expires_at=clock_timestamp()-interval '1 second' WHERE tenant_id=$1 AND holder_type='run' AND holder_id=$2", fixture.Tenant, defectStagePlan.shell.Run)
				case 2:
					_, err = defectPersonTx.Exec(ctx, "UPDATE photo.tasks SET cancel_requested=true WHERE tenant_id=$1 AND run_id=$2", fixture.Tenant, defectStagePlan.shell.Run)
				case 3:
					_, err = defectPersonTx.Exec(ctx, "UPDATE photo.runs SET cancel_requested=true WHERE tenant_id=$1 AND id=$2", fixture.Tenant, defectStagePlan.shell.Run)
				}
				if err != nil {
					t.Fatal("native budget mutation", changed, err)
				}
				var accepted bool
				if err := defectPersonTx.QueryRow(ctx, "SELECT photo.lock_semantic_defect_graph_budget($1,$2,$3::jsonb)", fixture.Tenant, defectStagePlan.shell.Run, string(admissionEvidenceBody)).Scan(&accepted); err != nil || accepted != (changed == 0) {
					t.Fatal("native graph budget", changed, accepted, err)
				}
				if _, err := defectPersonTx.Exec(ctx, "ROLLBACK TO SAVEPOINT native_budget_case"); err != nil {
					t.Fatal(err)
				}
				if _, err := defectPersonTx.Exec(ctx, "RELEASE SAVEPOINT native_budget_case"); err != nil {
					t.Fatal(err)
				}
			}
			t.Log("V27_DEFECT_SQL_GRAPH_BUDGET_OK")
			draftOutputRefs := []v2input.Ref{fixture.ManualFaceDefectPins.Labels, fixture.ManualFaceDefectPins.Protection, defectEvidence.LabelPNG, fixture.ManualFaceDefectPins.Proposal, fixture.ManualFaceDefectPins.Report}
			draftOutputBody, err := json.Marshal(draftOutputRefs)
			if err != nil {
				t.Fatal(err)
			}
			var draftResultAccepted bool
			if err := defectPersonTx.QueryRow(ctx, "SELECT photo.semantic_defect_native_result_ready($1,$2,0,0,$3::jsonb)", fixture.Tenant, defectStagePlan.shell.Tasks[0].ID, string(draftOutputBody)).Scan(&draftResultAccepted); err != nil || draftResultAccepted {
				t.Fatal("native draft result accepted", err)
			}
			t.Log("V27_DEFECT_NATIVE_DRAFT_RESULT_DENIED_OK")
			if _, err := defectPersonTx.Exec(ctx, "SET LOCAL ROLE photo_scheduler"); err != nil {
				t.Fatal(err)
			}
			t.Log("V27_DEFECT_SKIN_ROOT_LINEAGE_DENIED_OK")
			for changed := 0; changed < 5; changed++ {
				var candidate defectParserAuthority
				if err := json.Unmarshal(admissionEvidence.Parser, &candidate); err != nil {
					t.Fatal(err)
				}
				var roots []review.ProposalFaceMaskRoot
				if err := json.Unmarshal(candidate.Roots, &roots); err != nil || len(roots) == 0 {
					t.Fatal("mask root fixture required", err)
				}
				switch changed {
				case 1:
					roots = roots[:len(roots)-1]
				case 2:
					roots[0].Binding.Ordinal++
				case 3:
					roots[0].Binding.Landmarks.ID = uuid.New()
				case 4:
					roots = append(roots, roots[0])
				}
				candidate.Roots, _ = json.Marshal(roots)
				candidateBody, _ := json.Marshal(candidate)
				var matches bool
				if err := defectPersonTx.QueryRow(ctx, "SELECT photo.semantic_defect_mask_root_inventory_matches($1::jsonb,$2::jsonb,$3::jsonb)", string(admissionEvidence.Content), string(candidateBody), string(admissionEvidence.Manual)).Scan(&matches); err != nil || matches != (changed == 0) {
					defectPersonTx.Rollback(context.Background())
					t.Fatal("SQL mask root inventory", changed, err, matches)
				}
			}
			t.Log("V27_DEFECT_SQL_MASK_ROOT_INVENTORY_OK")
			for changed := 0; changed < 5; changed++ {
				var candidate []defectFaceAuthorityEntry
				if err := json.Unmarshal(admissionEvidence.Faces, &candidate); err != nil || len(candidate) == 0 {
					t.Fatal("landmark producer fixture required", err)
				}
				switch changed {
				case 1:
					candidate = []defectFaceAuthorityEntry{}
				case 2:
					candidate[0].Landmarks.ArtifactID = uuid.New()
				case 3:
					candidate[0].Landmarks.SHA256 = strings.Repeat("0", 64)
				case 4:
					candidate = append(candidate, candidate[0])
				}
				candidateBody, _ := json.Marshal(candidate)
				var matches bool
				if err := defectPersonTx.QueryRow(ctx, "SELECT photo.semantic_defect_landmark_inventory_matches($1::jsonb,$2::jsonb,$3::jsonb)", string(admissionEvidence.Files), string(admissionEvidence.Content), string(candidateBody)).Scan(&matches); err != nil || matches != (changed == 0) {
					defectPersonTx.Rollback(context.Background())
					t.Fatal("SQL landmark inventory", changed, err, matches)
				}
			}
			t.Log("V27_DEFECT_SQL_LANDMARK_INVENTORY_OK")
			for changed := 0; changed < 8; changed++ {
				var candidate defectRegistrationBinding
				if err := json.Unmarshal(admissionEvidence.Registration, &candidate); err != nil {
					t.Fatal(err)
				}
				var rows []v2input.Entry
				if err := json.Unmarshal(candidate.Transport.Rows, &rows); err != nil {
					t.Fatal(err)
				}
				switch changed {
				case 1:
					rows[0].Ref.SHA256 = strings.Repeat("0", 64)
				case 2:
					rows[0].Contract = "InvalidContract"
				case 3:
					for index, row := range rows {
						if row.Ref == packetContent.WorkingImage {
							rows = append(rows[:index], rows[index+1:]...)
							break
						}
					}
					candidate.Transport.Count--
				case 4:
					var direct []v2input.Ref
					if err := json.Unmarshal(candidate.Transport.Direct, &direct); err != nil {
						t.Fatal(err)
					}
					direct[1] = direct[0]
					candidate.Transport.Direct, _ = json.Marshal(direct)
				case 5:
					candidate.Transport.EntriesSHA = strings.Repeat("0", 64)
				case 6:
					candidate.InputDigest = strings.Repeat("0", 64)
				case 7:
					rows[0], rows[1] = rows[1], rows[0]
				}
				candidate.Transport.Rows, _ = json.Marshal(rows)
				candidateBody, _ := json.Marshal(candidate)
				var matches bool
				if err := defectPersonTx.QueryRow(ctx, "SELECT photo.semantic_defect_admission_closure_matches($1::jsonb,$2::jsonb,$3::jsonb)", string(admissionEvidence.Files), string(admissionEvidence.Content), string(candidateBody)).Scan(&matches); err != nil || matches != (changed == 0) {
					defectPersonTx.Rollback(context.Background())
					t.Fatal("SQL native closure consistency", changed, err, matches)
				}
			}
			t.Log("V27_DEFECT_SQL_CLOSURE_MATCH_OK")
			for _, role := range []string{"photo_scheduler", "photo_api", "photo_worker", "photo_publisher", "photo_portal", "photo_auth", "photo_gateway", "photo_maintenance", "photo_semantic_admission"} {
				functions := []string{"lock_semantic_defect_completed_base", "lock_semantic_defect_completed_evidence", "seal_semantic_defect_preparation_pre_completed", "lock_semantic_defect_admission_evidence_pre_closure", "semantic_defect_admission_closure_matches_pre_digest", "lock_semantic_defect_admission_evidence_pre_landmarks", "lock_semantic_defect_admission_evidence_pre_roots", "lock_semantic_defect_admission_evidence_pre_skin", "lock_semantic_defect_skin_lineage", "lock_semantic_defect_admission_evidence_pre_policy", "semantic_defect_policy_expectations_match", "lock_semantic_defect_admission_evidence_pre_anchor", "lock_semantic_defect_admission_anchor", "lock_semantic_defect_admission_evidence_pre_budget", "lock_semantic_defect_graph_budget", "semantic_defect_native_result_ready"}
				if role != "photo_scheduler" {
					functions = append(functions, "lock_semantic_defect_admission_evidence", "semantic_defect_admission_closure_matches", "semantic_defect_landmark_inventory_matches", "semantic_defect_mask_root_inventory_matches", "seal_semantic_defect_preparation")
				}
				for _, name := range functions {
					roleTx, err := evidenceOwner.Begin(ctx)
					if err != nil {
						t.Fatal(err)
					}
					if _, err := roleTx.Exec(ctx, "SET LOCAL ROLE "+pgx.Identifier{role}.Sanitize()); err != nil {
						roleTx.Rollback(context.Background())
						t.Fatal(err)
					}
					arguments := "NULL::jsonb,NULL::jsonb,NULL::jsonb"
					if strings.HasPrefix(name, "lock_") || strings.HasPrefix(name, "seal_") {
						arguments = "NULL::uuid,NULL::uuid,NULL::jsonb"
					}
					if name == "lock_semantic_defect_skin_lineage" {
						arguments = "NULL::uuid,NULL::jsonb"
					}
					if name == "semantic_defect_policy_expectations_match" {
						arguments = "NULL::jsonb"
					}
					if name == "semantic_defect_native_result_ready" {
						arguments = "NULL::uuid,NULL::uuid,NULL::integer,NULL::bigint,NULL::jsonb"
					}
					_, err = roleTx.Exec(ctx, "SELECT photo."+pgx.Identifier{name}.Sanitize()+"("+arguments+")")
					roleTx.Rollback(context.Background())
					var denied *pgconn.PgError
					if !errors.As(err, &denied) || denied.Code != "42501" {
						t.Fatal("native composed admission ACL bypass", role, name, err)
					}
				}
			}
			t.Log("V27_DEFECT_COMPOSED_ADMISSION_ACL_OK")

			for _, field := range []string{"tenant_id", "project_id", "actor_id", "session_id", "run_id", "params_document_id", "parent_run_id", "request_sha", "config_text", "policy_text", "unexpected"} {
				var candidate map[string]json.RawMessage
				if err := json.Unmarshal(admissionEvidenceBody, &candidate); err != nil {
					t.Fatal(err)
				}
				value := uuid.NewString()
				switch field {
				case "request_sha":
					value = strings.Repeat("0", 64)
				case "config_text", "policy_text":
					var original string
					if err := json.Unmarshal(candidate[field], &original); err != nil {
						t.Fatal(err)
					}
					value = original + " "
				}
				candidate[field], err = json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				changedBody, err := json.Marshal(candidate)
				if err != nil {
					t.Fatal(err)
				}
				var accepted bool
				if err := defectPersonTx.QueryRow(ctx, "SELECT photo.lock_semantic_defect_admission_evidence($1,$2,$3::jsonb)", fixture.Tenant, defectStagePlan.shell.Run, string(changedBody)).Scan(&accepted); err != nil || accepted {
					defectPersonTx.Rollback(context.Background())
					t.Fatal("composed SQL admission accepted substituted field", field, err)
				}
			}
			if _, err := defectPersonTx.Exec(ctx, "SAVEPOINT native_seal_case"); err != nil {
				t.Fatal(err)
			}
			// Only the disposable fixture enables dispatch compatibility, and rolls
			// back the capability, activation, ticket and events together afterward.
			if _, err := defectPersonTx.Exec(ctx, "SET LOCAL ROLE NONE"); err != nil {
				t.Fatal(err)
			}
			if _, err := defectPersonTx.Exec(ctx, "UPDATE photo.operation_registry SET v2_enabled=true WHERE operation='defects.propose' AND version='1'"); err != nil {
				t.Fatal(err)
			}
			if _, err := defectPersonTx.Exec(ctx, "SET LOCAL ROLE photo_scheduler"); err != nil {
				t.Fatal(err)
			}
			firstTicket, err := reader.sealDefectPreparationInTransaction(ctx, defectPersonTx, who, defectPersonAdmission, defectTransport.Graph.Built)
			if err != nil {
				t.Fatal("native dedicated seal", err)
			}
			if _, err := defectPersonTx.Exec(ctx, "SELECT set_config('app.tenant_id','',true)"); err != nil {
				t.Fatal(err)
			}
			replayTicket, err := reader.sealDefectPreparationInTransaction(ctx, defectPersonTx, who, defectPersonAdmission, defectTransport.Graph.Built)
			if err != nil || replayTicket != firstTicket {
				t.Fatal("native dedicated seal replay", err)
			}
			if _, err := defectPersonTx.Exec(ctx, "SET LOCAL ROLE NONE"); err != nil {
				t.Fatal(err)
			}
			var nativeRunState, nativeTaskState string
			var nativeSealed bool
			if err := defectPersonTx.QueryRow(ctx, "SELECT r.status,r.graph_sealed,t.state FROM photo.runs r JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id WHERE r.tenant_id=$1 AND r.id=$2", fixture.Tenant, defectStagePlan.shell.Run).Scan(&nativeRunState, &nativeSealed, &nativeTaskState); err != nil || nativeRunState != "queued" || !nativeSealed || nativeTaskState != "ready" {
				t.Fatal("native seal state", nativeRunState, nativeSealed, nativeTaskState, err)
			}
			nativeWorker := uuid.New()
			nativeNode := "native-defect-" + uuid.NewString()
			if _, err := defectPersonTx.Exec(ctx, "INSERT INTO photo.resource_nodes(node_id,enabled,ram_budget_bytes,vram_budget_bytes,scratch_budget_bytes,ram_available_bytes,vram_available_bytes,scratch_available_bytes,reported_at,doctor_report_sha256) VALUES($1,true,8589934592,0,8589934592,8589934592,0,8589934592,clock_timestamp(),$2)", nativeNode, strings.Repeat("a", 64)); err != nil {
				t.Fatal(err)
			}
			if _, err := defectPersonTx.Exec(ctx, `INSERT INTO photo.worker_registrations(tenant_id,worker_id,resource_classes,backends,lock_id,last_seen,capacity,db_principal,node_id,enabled,qualified_operations,v2_renderer_versions,certificate_sha256) VALUES($1,$2,'["cpu"]','["cpu"]',$3,clock_timestamp(),'{}',session_user,$4,true,'{"defects.propose":["cpu"]}','["v2.2"]',$5)`, fixture.Tenant, nativeWorker, strings.Repeat("a", 64), nativeNode, strings.Repeat("f", 64)); err != nil {
				t.Fatal(err)
			}
			if _, err := defectPersonTx.Exec(ctx, "INSERT INTO photo.resource_slots(node_id,slot_name,resource_class,capacity) VALUES($1,'cpu1','cpu','{}')", nativeNode); err != nil {
				t.Fatal(err)
			}
			var nativeEstimate struct {
				Resources struct {
					RAM     int64 `json:"ram_bytes"`
					VRAM    int64 `json:"vram_bytes"`
					Scratch int64 `json:"scratch_bytes"`
				} `json:"resource_estimate"`
			}
			if err := json.Unmarshal(defectStagePlan.shell.Tasks[0].ExecutionProfile, &nativeEstimate); err != nil {
				t.Fatal(err)
			}
			if _, err := defectPersonTx.Exec(ctx, "SET LOCAL ROLE photo_scheduler"); err != nil {
				t.Fatal(err)
			}
			var dispatchOutcome string
			var nativeCommand []byte
			if err := defectPersonTx.QueryRow(ctx, "SELECT * FROM photo.reserve_dispatch($1,$2,$3,$4,$5,$6,$7,$8)", defectStagePlan.shell.Tasks[0].ID, nativeNode, nativeEstimate.Resources.RAM, nativeEstimate.Resources.VRAM, nativeEstimate.Resources.Scratch, uuid.New(), uuid.New(), "").Scan(&dispatchOutcome, &nativeCommand); err != nil || dispatchOutcome != "queued" {
				t.Fatal("native dispatch", dispatchOutcome, err)
			}
			if _, err := defectPersonTx.Exec(ctx, "SET LOCAL ROLE photo_worker"); err != nil {
				t.Fatal(err)
			}
			var claimOutcome string
			var nativeFence int64
			var nativeLeaseUntil, nativeDeadline time.Time
			var nativeLeaseContext []byte
			if err := defectPersonTx.QueryRow(ctx, "SELECT * FROM photo.claim_task($1,1,$2,$3::jsonb)", defectStagePlan.shell.Tasks[0].ID, nativeWorker, string(nativeCommand)).Scan(&claimOutcome, &nativeFence, &nativeLeaseUntil, &nativeDeadline, &nativeLeaseContext); err != nil || claimOutcome != "claimed" || nativeFence < 1 || nativeLeaseUntil.After(nativeDeadline) || len(nativeLeaseContext) == 0 {
				t.Fatal("native claim", claimOutcome, nativeFence, err)
			}
			if _, err := defectPersonTx.Exec(ctx, "SET LOCAL ROLE NONE"); err != nil {
				t.Fatal(err)
			}
			if err := defectPersonTx.QueryRow(ctx, "SELECT status FROM photo.runs WHERE tenant_id=$1 AND id=$2", fixture.Tenant, defectStagePlan.shell.Run).Scan(&nativeRunState); err != nil || nativeRunState != "running" {
				t.Fatal("native claim did not start run", nativeRunState, err)
			}
			var nativeInputRows struct {
				Transport struct {
					Rows []v2input.Entry `json:"rows"`
				} `json:"transport"`
			}
			if err := json.Unmarshal(admissionEvidence.Registration, &nativeInputRows); err != nil || len(nativeInputRows.Transport.Rows) < 3 {
				t.Fatal("native gateway input rows", err)
			}
			if _, err := defectPersonTx.Exec(ctx, "SET LOCAL ROLE photo_gateway"); err != nil {
				t.Fatal(err)
			}
			nativeInputPins := make([]v2input.Ref, 0, len(nativeInputRows.Transport.Rows)+1+len(defectTransport.Graph.Built.Chunks))
			for _, row := range nativeInputRows.Transport.Rows {
				nativeInputPins = append(nativeInputPins, row.Ref)
			}
			nativeInputPins = append(nativeInputPins, defectTransport.Graph.Built.Manifest)
			nativeInputPins = append(nativeInputPins, defectTransport.Graph.Built.Chunks...)
			for _, pin := range nativeInputPins {
				var count int
				var sha string
				if err := defectPersonTx.QueryRow(ctx, "SELECT count(*),coalesce(max(sha256),'') FROM photo.gateway_input($1,1,$2,$3,$4,$5)", defectStagePlan.shell.Tasks[0].ID, nativeFence, nativeWorker, strings.Repeat("f", 64), pin.ArtifactID).Scan(&count, &sha); err != nil || count != 1 || sha != pin.SHA256 {
					t.Fatal("native gateway declared input", count, err)
				}
			}
			for changed := 0; changed < 3; changed++ {
				fence, worker, certificate := nativeFence, nativeWorker, strings.Repeat("f", 64)
				switch changed {
				case 0:
					fence++
				case 1:
					worker = uuid.New()
				case 2:
					certificate = strings.Repeat("0", 64)
				}
				var count int
				if err := defectPersonTx.QueryRow(ctx, "SELECT count(*) FROM photo.gateway_input($1,1,$2,$3,$4,$5)", defectStagePlan.shell.Tasks[0].ID, fence, worker, certificate, defectTransport.Graph.Built.Manifest.ArtifactID).Scan(&count); err != nil || count != 0 {
					t.Fatal("native gateway lease identity", changed, count, err)
				}
			}
			if _, err := defectPersonTx.Exec(ctx, "SET LOCAL ROLE NONE"); err != nil {
				t.Fatal(err)
			}
			// This is an upload/metadata contract test using real existing fixture
			// bytes. It does not execute the native algorithm or rebind report links.
			var uploadedNativeRefs []v2input.Ref
			for _, sourceRef := range draftOutputRefs {
				var sourceObject objectstore.Object
				var outputRole string
				if err := defectPersonTx.QueryRow(ctx, "SELECT object_key,object_version,sha256,bytes,media_type,output_role FROM photo.artifacts WHERE tenant_id=$1 AND id=$2", fixture.Tenant, sourceRef.ArtifactID).Scan(&sourceObject.Key, &sourceObject.VersionID, &sourceObject.SHA256, &sourceObject.Bytes, &sourceObject.ContentType, &outputRole); err != nil || sourceObject.SHA256 != sourceRef.SHA256 {
					t.Fatal("native upload source", err)
				}
				if _, err := defectPersonTx.Exec(ctx, "SET LOCAL ROLE photo_gateway"); err != nil {
					t.Fatal(err)
				}
				var outcome, targetKey, targetSHA string
				var uploadID, artifactID uuid.UUID
				if err := defectPersonTx.QueryRow(ctx, "SELECT * FROM photo.begin_output($1,1,$2,$3,$4,$5,$6,$7,$8,$9,$10)", defectStagePlan.shell.Tasks[0].ID, nativeFence, nativeWorker, strings.Repeat("f", 64), uuid.New(), outputRole, sourceObject.Bytes, sourceObject.SHA256, sourceObject.ContentType, fixture.Objects.Bucket).Scan(&outcome, &uploadID, &artifactID, &targetKey, &targetSHA); err != nil || outcome != "created" || targetSHA != sourceObject.SHA256 {
					t.Fatal("native begin output", outcome, err)
				}
				body, err := objects.Open(ctx, sourceObject)
				if err != nil {
					t.Fatal("native output bytes", err)
				}
				stored, putErr := objects.PutImmutable(ctx, targetKey, body, sourceObject.Bytes, sourceObject.SHA256, sourceObject.ContentType)
				closeErr := body.Close()
				if putErr != nil || closeErr != nil || stored.VersionID == "" || stored.VersionID == "null" {
					t.Fatal("native immutable upload", putErr, closeErr)
				}
				var completedID uuid.UUID
				var completedSHA string
				if err := defectPersonTx.QueryRow(ctx, "SELECT * FROM photo.complete_output($1,1,$2,$3,$4,$5,$6,$7,$8,$9)", defectStagePlan.shell.Tasks[0].ID, nativeFence, nativeWorker, strings.Repeat("f", 64), uploadID, stored.VersionID, stored.Bytes, stored.SHA256, stored.ContentType).Scan(&outcome, &completedID, &completedSHA); err != nil || outcome != "committed" || completedID != artifactID || completedSHA != stored.SHA256 {
					t.Fatal("native complete output", outcome, err)
				}
				uploadedNativeRefs = append(uploadedNativeRefs, v2input.Ref{ArtifactID: completedID, SHA256: completedSHA})
				if _, err := defectPersonTx.Exec(ctx, "SET LOCAL ROLE NONE"); err != nil {
					t.Fatal(err)
				}
			}
			outputBody, err := json.Marshal(uploadedNativeRefs)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := defectPersonTx.Exec(ctx, "SET LOCAL ROLE photo_fn_owner"); err != nil {
				t.Fatal(err)
			}
			var nativeOutputsReady bool
			if err := defectPersonTx.QueryRow(ctx, "SELECT photo.semantic_defect_native_result_ready($1,$2,1,$3,$4::jsonb)", fixture.Tenant, defectStagePlan.shell.Tasks[0].ID, nativeFence, string(outputBody)).Scan(&nativeOutputsReady); err != nil || !nativeOutputsReady {
				t.Fatal("native five-upload metadata readiness", err)
			}
			if err := defectPersonTx.QueryRow(ctx, "SELECT photo.semantic_defect_native_result_ready($1,$2,1,$3,$4::jsonb)", fixture.Tenant, defectStagePlan.shell.Tasks[0].ID, nativeFence+1, string(outputBody)).Scan(&nativeOutputsReady); err != nil || nativeOutputsReady {
				t.Fatal("native stale output fence accepted", err)
			}
			if _, err := defectPersonTx.Exec(ctx, "SET LOCAL ROLE NONE"); err != nil {
				t.Fatal(err)
			}
			// Exercise the result trigger and lease commit contract. Fixture report
			// links remain original: this does not prove native content adoption.
			manifest, err := canonical.Marshal(map[string]any{"outputs": uploadedNativeRefs, "metrics": map[string]any{}, "review_required": true})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := defectPersonTx.Exec(ctx, "SET LOCAL ROLE photo_worker"); err != nil {
				t.Fatal(err)
			}
			var resultOutcome, resultSHA string
			var resultID uuid.UUID
			if err := defectPersonTx.QueryRow(ctx, "SELECT * FROM photo.commit_task_success($1,1,$2,$3,$4)", defectStagePlan.shell.Tasks[0].ID, nativeFence, nativeWorker, string(manifest)).Scan(&resultOutcome, &resultID, &resultSHA); err != nil || resultOutcome != "committed" || resultID == uuid.Nil || len(resultSHA) != 64 {
				t.Fatal("native TaskResult publication", resultOutcome, err)
			}
			var replayResultID uuid.UUID
			var replayResultSHA string
			if err := defectPersonTx.QueryRow(ctx, "SELECT * FROM photo.commit_task_success($1,1,$2,$3,$4)", defectStagePlan.shell.Tasks[0].ID, nativeFence, nativeWorker, string(manifest)).Scan(&resultOutcome, &replayResultID, &replayResultSHA); err != nil || resultOutcome != "replayed" || replayResultID != resultID || replayResultSHA != resultSHA {
				t.Fatal("native TaskResult replay", resultOutcome, err)
			}
			if _, err := defectPersonTx.Exec(ctx, "SET LOCAL ROLE NONE"); err != nil {
				t.Fatal(err)
			}
			var canonicalID uuid.UUID
			var results, slots, reservations int
			if err := defectPersonTx.QueryRow(ctx, "SELECT state,canonical_result_id FROM photo.tasks WHERE tenant_id=$1 AND id=$2", fixture.Tenant, defectStagePlan.shell.Tasks[0].ID).Scan(&nativeTaskState, &canonicalID); err != nil || nativeTaskState != "succeeded" || canonicalID != resultID {
				t.Fatal("native canonical result", nativeTaskState, err)
			}
			if err := defectPersonTx.QueryRow(ctx, "SELECT (SELECT count(*) FROM photo.task_results WHERE tenant_id=$1 AND task_id=$2),(SELECT count(*) FROM photo.resource_slots WHERE tenant_id=$1 AND task_id=$2),(SELECT count(*) FROM photo.resource_reservations WHERE tenant_id=$1 AND task_id=$2 AND state='reserved')", fixture.Tenant, defectStagePlan.shell.Tasks[0].ID).Scan(&results, &slots, &reservations); err != nil || results != 1 || slots != 0 || reservations != 0 {
				t.Fatal("native result resource release", results, slots, reservations, err)
			}
			if _, err := defectPersonTx.Exec(ctx, "SET LOCAL ROLE photo_api"); err != nil {
				t.Fatal(err)
			}
			var catalogResultID uuid.UUID
			var catalogOutputs, catalogParams []byte
			binding := defectStagePlan.request.Intent.Reference
			if err := defectPersonTx.QueryRow(ctx, "SELECT * FROM photo.semantic_defect_native_result_catalog($1,$2,$3,$4,$5,$6,$7,$8)", fixture.Tenant, reader.ProjectID, who.UserID, who.SessionID, binding.AssetVersionID, binding.Source.ArtifactID, binding.Source.SHA256, defectStagePlan.shell.Tasks[0].ID).Scan(&catalogResultID, &catalogOutputs, &catalogParams); err != nil || catalogResultID != resultID {
				t.Fatal("native result catalog", err)
			}
			var catalogRefs []v2input.Ref
			var catalogParamRef v2input.Ref
			if json.Unmarshal(catalogOutputs, &catalogRefs) != nil || len(catalogRefs) != 5 || json.Unmarshal(catalogParams, &catalogParamRef) != nil || catalogParamRef != defectStagePlan.params.params {
				t.Fatal("native catalog pins")
			}
			for i, ref := range catalogRefs {
				if ref != uploadedNativeRefs[i] {
					t.Fatal("native catalog output identity", i)
				}
			}
			var foreignRows int
			if err := defectPersonTx.QueryRow(ctx, "SELECT count(*) FROM photo.semantic_defect_native_result_catalog($1,$2,$3,$4,$5,$6,$7,$8)", uuid.New(), reader.ProjectID, who.UserID, who.SessionID, binding.AssetVersionID, binding.Source.ArtifactID, binding.Source.SHA256, defectStagePlan.shell.Tasks[0].ID).Scan(&foreignRows); err != nil || foreignRows != 0 {
				t.Fatal("native foreign tenant catalog", err)
			}
			if _, err := defectPersonTx.Exec(ctx, "SET LOCAL ROLE NONE"); err != nil {
				t.Fatal(err)
			}
			t.Log("V27_DEFECT_NATIVE_RESULT_CATALOG_OK")
			t.Log("V27_DEFECT_NATIVE_TASK_RESULT_METADATA_OK")
			t.Log("V27_DEFECT_NATIVE_GATEWAY_OUTPUT_METADATA_OK")
			t.Log("V27_DEFECT_NATIVE_GATEWAY_INPUTS_OK")
			t.Log("V27_DEFECT_NATIVE_DISPATCH_CLAIM_OK")
			var queuedEvents, queuedOutbox int
			if err := defectPersonTx.QueryRow(ctx, "SELECT (SELECT count(*) FROM photo.run_events WHERE tenant_id=$1 AND run_id=$2 AND type='v2.run.queued'),(SELECT count(*) FROM photo.outbox WHERE tenant_id=$1 AND routing_key='v2.run.queued' AND payload->>'run_id'=$2::text)", fixture.Tenant, defectStagePlan.shell.Run).Scan(&queuedEvents, &queuedOutbox); err != nil || queuedEvents != 1 || queuedOutbox != 1 {
				t.Fatal("native queued event multiplicity", queuedEvents, queuedOutbox, err)
			}
			if _, err := defectPersonTx.Exec(ctx, "ROLLBACK TO SAVEPOINT native_seal_case"); err != nil {
				t.Fatal(err)
			}
			if _, err := defectPersonTx.Exec(ctx, "RELEASE SAVEPOINT native_seal_case"); err != nil {
				t.Fatal(err)
			}
			t.Log("V27_DEFECT_NATIVE_SEAL_REPLAY_OK")
			defectPersonTx.Rollback(context.Background())
			t.Log("V27_DEFECT_COMPOSED_ADMISSION_SUBSTITUTION_DENIED_OK")
			t.Log("V27_DEFECT_PERSON_ADMISSION_PREPARATION_LOCK_OK")
			wrongRegisteredTx, err := defectResumePool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			err = reader.lockRegisteredDefectPersonAdmission(ctx, wrongRegisteredTx, who, defectPersonAdmission, defectWrongTransport)
			wrongRegisteredTx.Rollback(context.Background())
			if !errors.Is(err, v2input.ErrConflict) {
				t.Fatal("registered native admission accepted substituted manifest", err)
			}
			t.Log("V27_DEFECT_REGISTERED_PERSON_ADMISSION_OK")

			activationContent, err := marshalDefectActivationContent(ctx, defectStagePlan)
			if err != nil {
				t.Fatal(err)
			}
			var activationEnvelope defectActivationContent
			if err := json.Unmarshal(activationContent, &activationEnvelope); err != nil {
				t.Fatal(err)
			}
			for changed := 0; changed < 4; changed++ {
				candidate := activationEnvelope
				switch changed {
				case 1:
					candidate.WorkingImageText += " "
				case 2:
					candidate.OriginalParamsText += " "
				case 3:
					candidate.ParamsText += " "
				}
				body, err := canonical.Marshal(candidate)
				if err != nil {
					t.Fatal(err)
				}
				var matches bool
				if err := defectResumePool.QueryRow(ctx, "SELECT photo.semantic_defect_activation_content_matches($1)", json.RawMessage(body)).Scan(&matches); err != nil || matches != (changed == 0) {
					t.Fatal("native SQL activation content match", changed, matches, err)
				}
			}
			t.Log("V27_DEFECT_SQL_CONTENT_HASH_MATCH_OK")
			genericDefectTx, err := defectResumePool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			var genericDefectSealed bool
			if err := genericDefectTx.QueryRow(ctx, "SELECT photo.seal_v2_graph($1,$2)", fixture.Tenant, defectStagePlan.shell.Run).Scan(&genericDefectSealed); err != nil || genericDefectSealed {
				genericDefectTx.Rollback(context.Background())
				t.Fatal("native defect generic seal bypass", err)
			}
			sealWriterTx, err := evidenceOwner.Begin(ctx)
			if err != nil {
				genericDefectTx.Rollback(context.Background())
				t.Fatal(err)
			}
			if _, err := sealWriterTx.Exec(ctx, "SET LOCAL lock_timeout='250ms'"); err != nil {
				sealWriterTx.Rollback(context.Background())
				genericDefectTx.Rollback(context.Background())
				t.Fatal(err)
			}
			_, sealWriteErr := sealWriterTx.Exec(ctx, "UPDATE photo.runs SET revision=revision WHERE tenant_id=$1 AND id=$2", fixture.Tenant, defectStagePlan.shell.Run)
			sealWriterTx.Rollback(context.Background())
			var sealWriteDenied *pgconn.PgError
			if !errors.As(sealWriteErr, &sealWriteDenied) || sealWriteDenied.Code != "55P03" {
				genericDefectTx.Rollback(context.Background())
				t.Fatal("native generic guard did not fence run", sealWriteErr)
			}
			genericDefectTx.Rollback(context.Background())
			if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.runs SET revision=revision WHERE tenant_id=$1 AND id=$2", fixture.Tenant, defectStagePlan.shell.Run); err != nil {
				t.Fatal("native generic guard did not release run", err)
			}
			t.Log("V27_DEFECT_GENERIC_SEAL_RUN_FENCE_OK")
			bypassDefectTx, err := defectResumePool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			_, bypassErr := bypassDefectTx.Exec(ctx, "SELECT photo.seal_v2_graph_pre_native_defect($1,$2)", fixture.Tenant, defectStagePlan.shell.Run)
			bypassDefectTx.Rollback(context.Background())
			var bypassDenied *pgconn.PgError
			if !errors.As(bypassErr, &bypassDenied) || bypassDenied.Code != "42501" {
				t.Fatal("native defect underlying seal callable", bypassErr)
			}
			t.Log("V27_DEFECT_GENERIC_SEAL_BYPASS_DENIED_OK")
			stateDraft, err := defectStagePlan.frozen.DraftTask()
			if err != nil {
				t.Fatal(err)
			}
			stateRequest := defectStagePlan.request
			stateRequest.RunID = defectStagePlan.shell.Run
			for changed := 0; changed < 5; changed++ {
				candidateRequest, candidateDraft, candidateGraph := stateRequest, stateDraft, defectTransport.Graph
				switch changed {
				case 1:
					candidateRequest.RunID = uuid.New()
				case 2:
					candidateDraft.ParamsSHA = strings.Repeat("0", 64)
				case 3:
					candidateDraft.InputDigest = strings.Repeat("0", 64)
				case 4:
					candidateGraph.ID = uuid.New()
				}
				stateTx, err := defectResumePool.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := stateTx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", fixture.Tenant.String()); err != nil {
					stateTx.Rollback(context.Background())
					t.Fatal(err)
				}
				state, err := defectPreparationRegistrationState(ctx, stateTx, candidateRequest, candidateDraft, candidateGraph)
				stateTx.Rollback(context.Background())
				expectedState := "conflict"
				if changed == 0 {
					expectedState = "replayed"
				}
				if err != nil || state != expectedState {
					t.Fatal("native defect registration state substitution", changed, state, err)
				}
			}
			t.Log("V27_DEFECT_REGISTRATION_STATE_SUBSTITUTION_DENIED_OK")
			decodedManual, err := reader.decodeDefectExecutionManualProofs(ctx, who, fixture.Tenant, request.Intent.Reference, preparedManualExecution)
			if err != nil || len(decodedManual) != 1 || decodedManual[0].objects.Pixel.ref != manualTarget {
				t.Fatal("retained manual execution decode", err)
			}
			// Exercise SQL authority independently of Go's retained-proof preflight.
			for changed := 0; changed < 8; changed++ {
				args := []any{fixture.Tenant, reader.ProjectID, who.UserID, who.SessionID, request.Intent.Reference.AssetVersionID, request.Intent.Reference.Source.ArtifactID, request.Intent.Reference.Source.SHA256}
				contextTenant := fixture.Tenant
				if changed < 6 {
					args[changed] = uuid.New()
				} else if changed == 6 {
					args[changed] = "0000000000000000000000000000000000000000000000000000000000000000"
				} else {
					contextTenant = uuid.New()
				}
				denialTx, err := manualLockPool.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = denialTx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", contextTenant.String()); err != nil {
					denialTx.Rollback(context.Background())
					t.Fatal(err)
				}
				var allowed bool
				err = denialTx.QueryRow(ctx, "SELECT photo.lock_semantic_defect_execution_scope($1,$2,$3,$4,$5,$6,$7)", args...).Scan(&allowed)
				denialTx.Rollback(context.Background())
				if err != nil || allowed {
					t.Fatal("defect execution SQL scope substitution accepted", changed, err)
				}
			}
			t.Log("V27_DEFECT_EXECUTION_SCOPE_SUBSTITUTION_DENIED_OK")
			manualExecutionTx, err := manualLockPool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if err = reader.lockDefectPolicyExecutionInTransaction(ctx, manualExecutionTx, who, fixture.Tenant, request.Intent.Reference, preparedPolicyExecution); err != nil {
				manualExecutionTx.Rollback(context.Background())
				t.Fatal("retained manual execution transaction", err)
			}

			if err := reader.lockDefectDeclaredFilesInTransaction(ctx, manualExecutionTx, who, fixture.Tenant, request.Intent.Reference, preparedManualExecution.face); err != nil {
				manualExecutionTx.Rollback(context.Background())
				t.Fatal("manual declared execution file locks", err)
			}
			t.Log("V27_DEFECT_DECLARED_EXECUTION_FILE_LOCKS_OK")
			t.Log("V27_DEFECT_EXECUTION_INVENTORY_TRANSACTION_OK")
			t.Log("V27_DEFECT_ORIGIN_EXECUTION_TRANSACTION_OK")
			t.Log("V27_DEFECT_POLICY_EXECUTION_TRANSACTION_OK")
			t.Log("V27_DEFECT_AUTHORIZED_EXECUTION_TRANSACTION_OK")
			manualExecutionWriter, err := evidenceOwner.Acquire(ctx)
			if err != nil {
				manualExecutionTx.Rollback(context.Background())
				t.Fatal(err)
			}
			if _, err = manualExecutionWriter.Exec(ctx, "SET lock_timeout='250ms'"); err != nil {
				manualExecutionWriter.Release()
				manualExecutionTx.Rollback(context.Background())
				t.Fatal(err)
			}
			_, manualWriteErr := manualExecutionWriter.Exec(ctx, "UPDATE photo.artifacts SET expires_at=expires_at WHERE id=$1", manualTarget.ArtifactID)

			originWriteQueries := []struct {
				SQL string
				ID  uuid.UUID
			}{
				{"UPDATE photo.artifacts SET expires_at=expires_at WHERE id=$1", preparedManualExecution.face.origin.Report.ArtifactID},
				{"UPDATE photo.task_attempts SET profile=profile WHERE task_id=(SELECT created_by_task_id FROM photo.artifacts WHERE id=$1)", preparedManualExecution.face.origin.Report.ArtifactID},
				{"UPDATE photo.users SET status=status WHERE id=$1", who.UserID},
				{"UPDATE photo.sessions SET revoked_at=revoked_at WHERE id=$1", who.SessionID},
			}
			for _, query := range originWriteQueries {
				_, writeErr := manualExecutionWriter.Exec(ctx, query.SQL, query.ID)
				var denied *pgconn.PgError
				if !errors.As(writeErr, &denied) || denied.Code != "55P03" {
					manualExecutionWriter.Exec(context.Background(), "RESET lock_timeout")
					manualExecutionWriter.Release()
					manualExecutionTx.Rollback(context.Background())
					t.Fatal("original producer mutation crossed transaction lock", writeErr)
				}
			}
			var manualWriteDenied *pgconn.PgError
			manualExecutionTx.Rollback(context.Background())
			if !errors.As(manualWriteErr, &manualWriteDenied) || manualWriteDenied.Code != "55P03" {
				manualExecutionWriter.Exec(context.Background(), "RESET lock_timeout")
				manualExecutionWriter.Release()
				t.Fatal("manual execution target write not blocked", manualWriteErr)
			}
			if _, err = manualExecutionWriter.Exec(ctx, "UPDATE photo.artifacts SET expires_at=expires_at WHERE id=$1", manualTarget.ArtifactID); err != nil {
				manualExecutionWriter.Exec(context.Background(), "RESET lock_timeout")
				manualExecutionWriter.Release()
				t.Fatal("manual execution rollback did not release target", err)
			}

			for _, query := range originWriteQueries {
				if _, err := manualExecutionWriter.Exec(ctx, query.SQL, query.ID); err != nil {
					manualExecutionWriter.Exec(context.Background(), "RESET lock_timeout")
					manualExecutionWriter.Release()
					t.Fatal("origin rollback did not release producer lock", err)
				}
			}
			t.Log("V27_DEFECT_ORIGIN_CONCURRENT_WRITE_DENIED_OK")
			t.Log("V27_DEFECT_AUTHORITY_CONCURRENT_WRITE_DENIED_OK")
			manualExecutionWriter.Exec(context.Background(), "RESET lock_timeout")
			manualExecutionWriter.Release()
			if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1", fixture.ManualFaceDefectPins.Report.ArtifactID); err != nil {
				t.Fatal(err)
			}
			t.Log("V27_DEFECT_MANUAL_EXECUTION_TRANSACTION_OK")

			lockSourceEvidence := func(proof sourcePreparationProof) error {
				tx, err := manualLockPool.Begin(ctx)
				if err != nil {
					return err
				}
				defer tx.Rollback(context.Background())
				if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", fixture.Tenant.String()); err != nil {
					return err
				}
				if err = reader.lockSourcePreparationProof(ctx, tx, who, fixture.Tenant, request.Intent.Reference, proof); err != nil {
					return err
				}
				writer, err := evidenceOwner.Acquire(ctx)
				if err != nil {
					return err
				}
				defer writer.Release()
				if _, err = writer.Exec(ctx, "SET lock_timeout='250ms'"); err != nil {
					return err
				}
				defer writer.Exec(context.Background(), "RESET lock_timeout")
				_, err = writer.Exec(ctx, "UPDATE photo.artifacts SET expires_at=expires_at WHERE id=$1", preparedSource.Inputs.Pixels.ArtifactID)
				var denied *pgconn.PgError
				if !errors.As(err, &denied) || denied.Code != "55P03" {
					t.Fatal("source RGB write not blocked", err)
				}
				return nil
			}
			if err := lockSourceEvidence(retainedSourceProof); err != nil {
				t.Fatal("source evidence locks", err)
			}
			changedSourcePins, err := decodeSourcePreparationPins(retainedSourceProof)
			if err != nil {
				t.Fatal(err)
			}
			changedSourcePins[1].Object.Key += "-changed"
			changedSourceRaw, err := canonical.Marshal(changedSourcePins)
			if err != nil {
				t.Fatal(err)
			}
			changedSourceProof := retainedSourceProof
			changedSourceProof.pins = string(changedSourceRaw)
			if err := lockSourceEvidence(changedSourceProof); !errors.Is(err, v2input.ErrConflict) {
				t.Fatal("changed source RGB locked", err)
			}
			t.Log("V27_SOURCE_EVIDENCE_LOCK_OK")
			sourceEvidenceBody, err := marshalSourceEvidencePins(retainedSourceProof)
			if err != nil {
				t.Fatal(err)
			}
			var sourceEvidenceEntries []json.RawMessage
			if err := json.Unmarshal(sourceEvidenceBody, &sourceEvidenceEntries); err != nil {
				t.Fatal(err)
			}
			for _, entries := range [][]json.RawMessage{{sourceEvidenceEntries[0], sourceEvidenceEntries[0]}, {sourceEvidenceEntries[1], sourceEvidenceEntries[1]}, {sourceEvidenceEntries[1], sourceEvidenceEntries[0]}} {
				tx, err := manualLockPool.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", fixture.Tenant.String()); err != nil {
					t.Fatal(err)
				}
				body, err := canonical.Marshal(entries)
				if err != nil {
					t.Fatal(err)
				}
				var ready bool
				err = tx.QueryRow(ctx, "SELECT photo.lock_semantic_proposal_source_evidence($1,$2,$3,$4,$5,$6,$7,$8)", fixture.Tenant, reader.ProjectID, who.UserID, who.SessionID, request.Intent.Reference.AssetVersionID, request.Intent.Reference.Source.ArtifactID, request.Intent.Reference.Source.SHA256, string(body)).Scan(&ready)
				tx.Rollback(context.Background())
				if err != nil || ready {
					t.Fatal("incomplete/reordered direct source pins accepted", err)
				}
			}
			t.Log("V27_SOURCE_EVIDENCE_SHAPE_OK")
			lockParserEvidence := func(proof faceParsePreparationProof) error {
				tx, err := manualLockPool.Begin(ctx)
				if err != nil {
					return err
				}
				defer tx.Rollback(context.Background())
				if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", fixture.Tenant.String()); err != nil {
					return err
				}
				if err = reader.lockFaceParseEvidenceProof(ctx, tx, who, fixture.Tenant, request.Intent.Reference, proof); err != nil {
					return err
				}
				writer, err := evidenceOwner.Acquire(ctx)
				if err != nil {
					return err
				}
				defer writer.Release()
				if _, err = writer.Exec(ctx, "SET lock_timeout='250ms'"); err != nil {
					return err
				}
				defer writer.Exec(context.Background(), "RESET lock_timeout")
				for _, id := range []uuid.UUID{proof.pixels.ref.ArtifactID, proof.evidence.objects[0][1].ref.ArtifactID, proof.evidence.objects[1][1].ref.ArtifactID} {
					_, err := writer.Exec(ctx, "UPDATE photo.artifacts SET expires_at=expires_at WHERE id=$1", id)
					var denied *pgconn.PgError
					if !errors.As(err, &denied) || denied.Code != "55P03" {
						t.Fatal("parser evidence write not blocked", err)
					}
				}
				return nil
			}
			if err := lockParserEvidence(retainedParseProof); err != nil {
				t.Fatal("parser evidence locks", err)
			}
			changedParserEvidence := retainedParseProof
			changedParserEvidence.pixels.object.Key += "-changed"
			if err := lockParserEvidence(changedParserEvidence); !errors.Is(err, v2input.ErrConflict) {
				t.Fatal("changed parser labels locked", err)
			}
			t.Log("V27_FACE_PARSE_EVIDENCE_LOCK_OK")
			lockParserQualification := func(proof faceParsePreparationProof) error {
				tx, err := manualLockPool.Begin(ctx)
				if err != nil {
					return err
				}
				defer tx.Rollback(context.Background())
				if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", fixture.Tenant.String()); err != nil {
					return err
				}
				if err = reader.lockFaceParseQualificationProof(ctx, tx, who, fixture.Tenant, request.Intent.Reference, proof); err != nil {
					return err
				}
				writer, err := evidenceOwner.Acquire(ctx)
				if err != nil {
					return err
				}
				defer writer.Release()
				if _, err = writer.Exec(ctx, "SET lock_timeout='250ms'"); err != nil {
					return err
				}
				defer writer.Exec(context.Background(), "RESET lock_timeout")
				for _, q := range proof.evidence.qualification {
					_, err := writer.Exec(ctx, "UPDATE photo.artifacts SET expires_at=expires_at WHERE id=$1", q.ref.ArtifactID)
					var denied *pgconn.PgError
					if !errors.As(err, &denied) || denied.Code != "55P03" {
						t.Fatal("parser report write not blocked", err)
					}
				}
				return nil
			}
			if err := lockParserQualification(retainedParseProof); err != nil {
				t.Fatal("parser qualification locks", err)
			}
			changedParserProof := retainedParseProof
			changedParserProof.evidence.qualification[0].object.Key += "-changed"
			if err := lockParserQualification(changedParserProof); !errors.Is(err, v2input.ErrConflict) {
				t.Fatal("changed parser report locked", err)
			}
			t.Log("V27_FACE_PARSE_QUALIFICATION_LOCK_OK")
			lockParserBatch := func(proofs []faceParsePreparationProof) error {
				tx, err := manualLockPool.Begin(ctx)
				if err != nil {
					return err
				}
				defer tx.Rollback(context.Background())
				if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", fixture.Tenant.String()); err != nil {
					return err
				}
				return reader.lockFaceParsePreparationProofs(ctx, tx, who, fixture.Tenant, request.Intent.Reference, proofs)
			}
			if err := lockParserBatch([]faceParsePreparationProof{retainedParseProof, retainedParseProof}); err != nil {
				t.Fatal("composed parser batch", err)
			}
			if err := lockParserBatch([]faceParsePreparationProof{changedParserEvidence}); !errors.Is(err, v2input.ErrConflict) {
				t.Fatal("changed labels batch", err)
			}
			if err := lockParserBatch([]faceParsePreparationProof{changedParserProof}); !errors.Is(err, v2input.ErrConflict) {
				t.Fatal("changed report batch", err)
			}
			t.Log("V27_FACE_PARSE_BATCH_TRANSACTION_LOCK_OK")
			parserInventoryTx, err := manualLockPool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = parserInventoryTx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", fixture.Tenant.String()); err != nil {
				parserInventoryTx.Rollback(context.Background())
				t.Fatal(err)
			}
			if err := reader.lockFaceParsePreparationInventory(ctx, parserInventoryTx, who, fixture.Tenant, request.Intent.Reference, []faceParsePreparationProof{retainedParseProof}); err != nil {
				parserInventoryTx.Rollback(context.Background())
				t.Fatal("exact parser inventory locks", err)
			}
			var emptyInventoryAccepted bool
			if err := parserInventoryTx.QueryRow(ctx, "SELECT photo.lock_semantic_proposal_face_parse_inventory($1,$2,$3,$4,$5,$6,$7,$8,$9)", fixture.Tenant, reader.ProjectID, who.UserID, who.SessionID, request.Intent.Reference.AssetVersionID, request.Intent.Reference.Source.ArtifactID, request.Intent.Reference.Source.SHA256, retainedParseProof.evidence.expected, "[]").Scan(&emptyInventoryAccepted); err != nil || emptyInventoryAccepted {
				parserInventoryTx.Rollback(context.Background())
				t.Fatal("empty parser inventory admitted", err)
			}
			parserInventoryBody, parserInventoryExpected, err := reader.marshalFaceParsePreparationInventory(who, fixture.Tenant, request.Intent.Reference, []faceParsePreparationProof{retainedParseProof})
			if err != nil {
				parserInventoryTx.Rollback(context.Background())
				t.Fatal(err)
			}
			changedInventoryBody := strings.ReplaceAll(string(parserInventoryBody), fixture.SkinLabels.SHA256, strings.Repeat("0", 64))
			var changedInventoryAccepted bool
			if err := parserInventoryTx.QueryRow(ctx, "SELECT photo.lock_semantic_proposal_face_parse_inventory($1,$2,$3,$4,$5,$6,$7,$8,$9)", fixture.Tenant, reader.ProjectID, who.UserID, who.SessionID, request.Intent.Reference.AssetVersionID, request.Intent.Reference.Source.ArtifactID, request.Intent.Reference.Source.SHA256, parserInventoryExpected, changedInventoryBody).Scan(&changedInventoryAccepted); err != nil || changedInventoryAccepted || changedInventoryBody == string(parserInventoryBody) {
				parserInventoryTx.Rollback(context.Background())
				t.Fatal("changed parser inventory admitted", err)
			}
			t.Log("V27_FACE_PARSE_INVENTORY_EXACT_PINS_OK")
			parserInventoryTx.Rollback(context.Background())
			t.Log("V27_FACE_PARSE_INVENTORY_TRANSACTION_LOCK_OK")
			parserSourceInventoryTx, err := manualLockPool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = parserSourceInventoryTx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", fixture.Tenant.String()); err != nil {
				parserSourceInventoryTx.Rollback(context.Background())
				t.Fatal(err)
			}
			if err := reader.lockFaceParseSourcePreparationInventory(ctx, parserSourceInventoryTx, who, fixture.Tenant, request.Intent.Reference, color, retainedSourceProof, []faceParsePreparationProof{retainedParseProof}); err != nil {
				parserSourceInventoryTx.Rollback(context.Background())
				t.Fatal("source/exact parser inventory locks", err)
			}
			parserWriter, err := evidenceOwner.Acquire(ctx)
			if err != nil {
				parserSourceInventoryTx.Rollback(context.Background())
				t.Fatal(err)
			}
			parserWriterStatements := []string{
				"INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at) SELECT $2,tenant_id,bucket,$3,sha256,bytes,media_type,object_version,verified_at FROM photo.artifacts WHERE id=$1",
				"INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,inputs,params_artifact_id,input_digest,execution_profile) SELECT $2,tenant_id,run_id,$3,operation,operation_version,resource_class,inputs,params_artifact_id,input_digest,execution_profile FROM photo.tasks WHERE id=(SELECT created_by_task_id FROM photo.artifacts WHERE id=$1)",
			}
			for _, statement := range parserWriterStatements {
				writerTx, err := parserWriter.Begin(ctx)
				if err != nil {
					parserWriter.Release()
					parserSourceInventoryTx.Rollback(context.Background())
					t.Fatal(err)
				}
				if _, err = writerTx.Exec(ctx, "SET LOCAL lock_timeout='250ms'"); err != nil {
					writerTx.Rollback(context.Background())
					parserWriter.Release()
					parserSourceInventoryTx.Rollback(context.Background())
					t.Fatal(err)
				}
				_, writeErr := writerTx.Exec(ctx, statement, fixture.SkinParse.ArtifactID, uuid.New(), uuid.NewString())
				writerTx.Rollback(context.Background())
				var denied *pgconn.PgError
				if !errors.As(writeErr, &denied) || denied.Code != "55P03" {
					parserWriter.Release()
					parserSourceInventoryTx.Rollback(context.Background())
					t.Fatal("new parser producer/artifact crossed inventory barrier", writeErr)
				}
			}
			t.Log("V27_FACE_PARSE_INVENTORY_PHANTOM_WRITER_OK")
			if err := parserSourceInventoryTx.Rollback(context.Background()); err != nil {
				parserWriter.Release()
				t.Fatal("release parser inventory transaction", err)
			}
			for _, statement := range parserWriterStatements {
				writerTx, err := parserWriter.Begin(ctx)
				if err != nil {
					parserWriter.Release()
					t.Fatal(err)
				}
				if _, err = writerTx.Exec(ctx, "SET LOCAL lock_timeout='250ms'"); err != nil {
					writerTx.Rollback(context.Background())
					parserWriter.Release()
					t.Fatal(err)
				}
				written, writeErr := writerTx.Exec(ctx, statement, fixture.SkinParse.ArtifactID, uuid.New(), uuid.NewString())
				writerTx.Rollback(context.Background())
				if writeErr != nil || written.RowsAffected() != 1 {
					parserWriter.Release()
					t.Fatal("parser writer failed after inventory release", writeErr, written.RowsAffected())
				}
			}
			parserWriter.Release()
			t.Log("V27_FACE_PARSE_INVENTORY_WRITER_RELEASE_OK")
			t.Log("V27_FACE_PARSE_SOURCE_INVENTORY_LOCK_OK")
			combinedTx, err := manualLockPool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = combinedTx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", fixture.Tenant.String()); err != nil {
				t.Fatal(err)
			}
			if err := reader.lockFaceParseSourcePreparationProofs(ctx, combinedTx, who, fixture.Tenant, request.Intent.Reference, color, retainedSourceProof, []faceParsePreparationProof{retainedParseProof, retainedParseProof}); err != nil {
				combinedTx.Rollback(context.Background())
				t.Fatal("combined source/parser locks", err)
			}
			combinedTx.Rollback(context.Background())
			t.Log("V27_FACE_PARSE_SOURCE_TRANSACTION_LOCK_OK")
			lockFaceEvidence := func(proof facePreparationProof) error {
				tx, err := manualLockPool.Begin(ctx)
				if err != nil {
					return err
				}
				defer tx.Rollback(context.Background())
				if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", fixture.Tenant.String()); err != nil {
					return err
				}
				if err := reader.lockFaceEvidenceProof(ctx, tx, who, fixture.Tenant, request.Intent.Reference, proof); err != nil {
					return err
				}
				writer, err := evidenceOwner.Acquire(ctx)
				if err != nil {
					return err
				}
				defer writer.Release()
				if _, err = writer.Exec(ctx, "SET lock_timeout='250ms'"); err != nil {
					return err
				}
				defer writer.Exec(context.Background(), "RESET lock_timeout")
				producerSelect := "SELECT created_by_task_id FROM photo.artifacts WHERE id=$1"
				for _, statement := range []string{
					"UPDATE photo.tasks SET state=state WHERE id=(" + producerSelect + ")",
					"UPDATE photo.task_attempts SET profile=profile WHERE task_id=(" + producerSelect + ")",
					"UPDATE photo.task_results SET metrics=metrics WHERE task_id=(" + producerSelect + ")",
					"UPDATE photo.artifacts SET expires_at=expires_at WHERE id=(SELECT params_artifact_id FROM photo.tasks WHERE id=(" + producerSelect + "))",
					"UPDATE photo.runs SET status=status WHERE id=(SELECT run_id FROM photo.tasks WHERE id=(" + producerSelect + "))",
				} {
					_, err := writer.Exec(ctx, statement, proof.objects[0][0].ref.ArtifactID)
					var denied *pgconn.PgError
					if !errors.As(err, &denied) || denied.Code != "55P03" {
						t.Fatal("concurrent face producer write was not blocked", err)
					}
				}
				return nil
			}
			if err := lockFaceEvidence(retainedFaceProof); err != nil {
				t.Fatal("original face producer/Params evidence locks", err)
			}
			changedFaceEvidence := retainedFaceProof
			changedFaceEvidence.objects[0][1].object.Key += "-changed"
			if err := lockFaceEvidence(changedFaceEvidence); !errors.Is(err, v2input.ErrConflict) {
				t.Fatal("changed landmark Params locked", err)
			}
			t.Log("V27_FACE_EVIDENCE_LOCK_OK")
			lockFaceQualification := func(proof facePreparationProof) error {
				tx, err := manualLockPool.Begin(ctx)
				if err != nil {
					return err
				}
				defer tx.Rollback(context.Background())
				if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", fixture.Tenant.String()); err != nil {
					return err
				}
				if err := reader.lockFaceQualificationProof(ctx, tx, who, fixture.Tenant, request.Intent.Reference, proof); err != nil {
					return err
				}
				writer, err := evidenceOwner.Acquire(ctx)
				if err != nil {
					return err
				}
				defer writer.Release()
				if _, err = writer.Exec(ctx, "SET lock_timeout='250ms'"); err != nil {
					return err
				}
				defer writer.Exec(context.Background(), "RESET lock_timeout")
				for _, statement := range []string{
					"UPDATE photo.artifacts SET expires_at=expires_at WHERE id=$1",
					"UPDATE photo.model_qualifications SET status=status WHERE report_artifact_id=$1",
					"UPDATE photo.v2_qualification_policies policy SET admission_enabled=policy.admission_enabled FROM photo.model_qualifications q,photo.audit_log log WHERE q.report_artifact_id=$1 AND log.tenant_id=q.tenant_id AND log.resource_id=q.id AND log.action='v2_qualification.imported' AND policy.id::text=log.details->>'policy_id'",
				} {
					_, err := writer.Exec(ctx, statement, proof.qualification[0].ref.ArtifactID)
					var denied *pgconn.PgError
					if !errors.As(err, &denied) || denied.Code != "55P03" {
						t.Fatal("concurrent face qualification write was not blocked", err)
					}
				}
				return nil
			}
			if err := lockFaceQualification(retainedFaceProof); err != nil {
				t.Fatal("exact face qualification policy/report locks", err)
			}
			changedFaceProof := retainedFaceProof
			changedFaceProof.qualification[1].object.Key += "-changed"
			if err := lockFaceQualification(changedFaceProof); !errors.Is(err, v2input.ErrConflict) {
				t.Fatal("changed face report pin locked", err)
			}
			t.Log("V27_FACE_QUALIFICATION_LOCK_OK")

			lockFaceBatch := func(proofs []facePreparationProof) error {
				tx, err := manualLockPool.Begin(ctx)
				if err != nil {
					return err
				}
				defer tx.Rollback(context.Background())
				if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", fixture.Tenant.String()); err != nil {
					return err
				}
				if err = reader.lockFacePreparationProofs(ctx, tx, who, fixture.Tenant, request.Intent.Reference, proofs); err != nil {
					return err
				}
				writer, err := evidenceOwner.Acquire(ctx)
				if err != nil {
					return err
				}
				defer writer.Release()
				if _, err = writer.Exec(ctx, "SET lock_timeout='250ms'"); err != nil {
					return err
				}
				defer writer.Exec(context.Background(), "RESET lock_timeout")
				for _, id := range []uuid.UUID{retainedFaceProof.objects[0][1].ref.ArtifactID, retainedFaceProof.objects[1][1].ref.ArtifactID, retainedFaceProof.qualification[0].ref.ArtifactID, retainedFaceProof.qualification[1].ref.ArtifactID} {
					_, err = writer.Exec(ctx, "UPDATE photo.artifacts SET expires_at=expires_at WHERE id=$1", id)
					var denied *pgconn.PgError
					if !errors.As(err, &denied) || denied.Code != "55P03" {
						t.Fatal("composed face evidence/report write was not blocked", err)
					}
				}
				return nil
			}
			if err := lockFaceBatch([]facePreparationProof{retainedFaceProof, retainedFaceProof}); err != nil {
				t.Fatal("composed face batch locks", err)
			}
			if err := lockFaceBatch([]facePreparationProof{changedFaceEvidence}); !errors.Is(err, v2input.ErrConflict) {
				t.Fatal("changed face batch Params admitted", err)
			}
			if err := lockFaceBatch([]facePreparationProof{changedFaceProof}); !errors.Is(err, v2input.ErrConflict) {
				t.Fatal("changed face batch qualification admitted", err)
			}
			t.Log("V27_FACE_BATCH_TRANSACTION_LOCK_OK")
			inventoryProofs := make([]facePreparationProof, len(preparedFaceInventory))
			for i, collection := range preparedFaceInventory {
				inventoryProofs[i] = collection.proof
			}
			lockDiscoveredFaces := func(proofs []facePreparationProof) error {
				tx, err := manualLockPool.Begin(ctx)
				if err != nil {
					return err
				}
				defer tx.Rollback(context.Background())
				if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", fixture.Tenant.String()); err != nil {
					return err
				}
				if err := reader.lockFaceSourcePreparationInventory(ctx, tx, who, fixture.Tenant, request.Intent.Reference, color, retainedSourceProof, proofs); err != nil {
					return err
				}
				writer, err := evidenceOwner.Acquire(ctx)
				if err != nil {
					return err
				}
				defer writer.Release()
				for _, statement := range []string{
					"INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at) SELECT $2,tenant_id,bucket,$3,sha256,bytes,media_type,object_version,verified_at FROM photo.artifacts WHERE id=$1",
					"INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,inputs,params_artifact_id,input_digest,execution_profile) SELECT $2,tenant_id,run_id,$3,operation,operation_version,resource_class,inputs,params_artifact_id,input_digest,execution_profile FROM photo.tasks WHERE id=(SELECT created_by_task_id FROM photo.artifacts WHERE id=$1)",
				} {
					writerTx, err := writer.Begin(ctx)
					if err != nil {
						return err
					}
					if _, err = writerTx.Exec(ctx, "SET LOCAL lock_timeout='250ms'"); err != nil {
						writerTx.Rollback(context.Background())
						return err
					}
					_, writeErr := writerTx.Exec(ctx, statement, proofs[0].objects[0][0].ref.ArtifactID, uuid.New(), uuid.NewString())
					writerTx.Rollback(context.Background())
					var denied *pgconn.PgError
					if !errors.As(writeErr, &denied) || denied.Code != "55P03" {
						t.Fatal("new face producer/artifact write crossed inventory barrier", writeErr)
					}
				}
				return nil
			}
			if err := lockDiscoveredFaces(inventoryProofs); err != nil {
				t.Fatal("discovered face inventory locks", err)
			}
			if err := lockDiscoveredFaces(inventoryProofs[:1]); !errors.Is(err, v2input.ErrConflict) {
				t.Fatal("omitted face collection admitted", err)
			}
			t.Log("V27_FACE_INVENTORY_TRANSACTION_LOCK_OK")
			t.Log("V27_FACE_INVENTORY_PHANTOM_WRITER_OK")
			t.Log("V27_FACE_SOURCE_INVENTORY_LOCK_OK")
			lockAllFaceFamilies := func(faces []facePreparationProof, parsers []faceParsePreparationProof) error {
				tx, err := manualLockPool.Begin(ctx)
				if err != nil {
					return err
				}
				defer tx.Rollback(context.Background())
				if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", fixture.Tenant.String()); err != nil {
					return err
				}
				return reader.lockFaceAndParseSourceInventory(ctx, tx, who, fixture.Tenant, request.Intent.Reference, color, retainedSourceProof, faces, parsers)
			}
			if err := lockAllFaceFamilies(inventoryProofs, []faceParsePreparationProof{retainedParseProof}); err != nil {
				t.Fatal("all face/parser families", err)
			}
			if err := lockAllFaceFamilies(inventoryProofs[:1], []faceParsePreparationProof{retainedParseProof}); !errors.Is(err, v2input.ErrConflict) {
				t.Fatal("all-family omitted face admitted", err)
			}
			changedAllParser := retainedParseProof
			changedAllParser.pixels.object.Key += "-changed"
			if err := lockAllFaceFamilies(inventoryProofs, []faceParsePreparationProof{changedAllParser}); !errors.Is(err, v2input.ErrConflict) {
				t.Fatal("all-family changed parser admitted", err)
			}
			t.Log("V27_FACE_AND_PARSE_INVENTORY_REJECTION_OK")
			t.Log("V27_FACE_AND_PARSE_SOURCE_INVENTORY_LOCK_OK")
			lockManualFamilies := func(proofs []manualPreparationProof) error {
				tx, err := manualLockPool.Begin(ctx)
				if err != nil {
					return err
				}
				defer tx.Rollback(context.Background())
				if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", fixture.Tenant.String()); err != nil {
					return err
				}
				return reader.lockFaceParseManualSourceInventory(ctx, tx, who, fixture.Tenant, request.Intent.Reference, color, retainedSourceProof, inventoryProofs, []faceParsePreparationProof{retainedParseProof}, proofs)
			}
			if err := lockManualFamilies([]manualPreparationProof{retainedManualProof}); err != nil {
				t.Fatal("manual common family locks", err)
			}
			changedFamilyManual := retainedManualProof
			changedFamilyManual.objects.Params.object.Key += "-changed"
			if err := lockManualFamilies([]manualPreparationProof{changedFamilyManual}); !errors.Is(err, v2input.ErrConflict) {
				t.Fatal("changed manual common family accepted", err)
			}
			if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1", retainedManualProof.objects.Root.ref.ArtifactID); err != nil {
				t.Fatal(err)
			}
			if err := lockManualFamilies([]manualPreparationProof{retainedManualProof}); !errors.Is(err, v2input.ErrConflict) {
				t.Fatal("expired manual common family accepted", err)
			}
			if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=NULL WHERE id=$1", retainedManualProof.objects.Root.ref.ArtifactID); err != nil {
				t.Fatal(err)
			}
			if err := lockManualFamilies([]manualPreparationProof{retainedManualProof}); err != nil {
				t.Fatal("restored manual common family", err)
			}
			t.Log("V27_FACE_PARSE_MANUAL_REJECTION_OK")
			t.Log("V27_FACE_PARSE_MANUAL_SOURCE_LOCK_OK")
			lockPersonFamilies := func(proofs []personProtectionProof) error {
				tx, err := manualLockPool.Begin(ctx)
				if err != nil {
					return err
				}
				defer tx.Rollback(context.Background())
				if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", fixture.Tenant.String()); err != nil {
					return err
				}
				return reader.lockSelectedPersonSourceInventory(ctx, tx, who, fixture.Tenant, request.Intent.Reference, color, retainedSourceProof, inventoryProofs, []faceParsePreparationProof{retainedParseProof}, []manualPreparationProof{retainedManualProof}, proofs)
			}
			if err := lockPersonFamilies([]personProtectionProof{retainedCoverageProof}); err != nil {
				t.Fatal("person shared family locks", err)
			}
			if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=$1", fixture.PersonQualification.PolicyID); err != nil {
				t.Fatal(err)
			}
			if err := lockPersonFamilies([]personProtectionProof{retainedCoverageProof}); !errors.Is(err, v2input.ErrConflict) {
				t.Fatal("revoked person shared policy accepted", err)
			}
			if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.v2_qualification_policies SET admission_enabled=true WHERE id=$1", fixture.PersonQualification.PolicyID); err != nil {
				t.Fatal(err)
			}
			if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1", fixture.PersonQualification.Report.ArtifactID); err != nil {
				t.Fatal(err)
			}
			if err := lockPersonFamilies([]personProtectionProof{retainedCoverageProof}); !errors.Is(err, v2input.ErrConflict) {
				t.Fatal("expired person shared qualification accepted", err)
			}
			if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=NULL WHERE id=$1", fixture.PersonQualification.Report.ArtifactID); err != nil {
				t.Fatal(err)
			}
			if err := lockPersonFamilies([]personProtectionProof{retainedCoverageProof}); err != nil {
				t.Fatal("restored person shared qualification", err)
			}
			t.Log("V27_PERSON_SHARED_QUALIFICATION_REJECTION_OK")
			t.Log("V27_PERSON_SHARED_SOURCE_INVENTORY_LOCK_OK")
			policyInventory, err := reader.qualificationPolicyInventory(ctx, who, fixture.Tenant)
			if err != nil {
				t.Fatal("global policy inventory", err)
			}
			policyBarrierTx, err := manualLockPool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = policyBarrierTx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", fixture.Tenant.String()); err != nil {
				policyBarrierTx.Rollback(context.Background())
				t.Fatal(err)
			}
			var policyLocked bool
			if err := policyBarrierTx.QueryRow(ctx, "SELECT photo.lock_semantic_proposal_qualification_inventory($1,$2,$3,$4)", fixture.Tenant, reader.ProjectID, who.UserID, who.SessionID).Scan(&policyLocked); err != nil || !policyLocked {
				policyBarrierTx.Rollback(context.Background())
				t.Fatal("global policy barrier", err)
			}
			var policySnapshotAccepted bool
			if err := policyBarrierTx.QueryRow(ctx, "SELECT photo.lock_semantic_proposal_qualification_policy_snapshot($1,$2,$3,$4,$5)", fixture.Tenant, reader.ProjectID, who.UserID, who.SessionID, policyInventory).Scan(&policySnapshotAccepted); err != nil || !policySnapshotAccepted {
				policyBarrierTx.Rollback(context.Background())
				t.Fatal("exact global policy snapshot", err)
			}
			if err := policyBarrierTx.QueryRow(ctx, "SELECT photo.lock_semantic_proposal_qualification_policy_snapshot($1,$2,$3,$4,$5)", fixture.Tenant, reader.ProjectID, who.UserID, who.SessionID, "[]").Scan(&policySnapshotAccepted); err != nil || policySnapshotAccepted {
				policyBarrierTx.Rollback(context.Background())
				t.Fatal("empty global policy snapshot", err)
			}
			t.Log("V27_GLOBAL_POLICY_SNAPSHOT_OK")
			policyStatements := []string{
				"UPDATE photo.v2_qualification_policies SET enabled=enabled WHERE id=$1 AND $2::uuid IS NOT NULL AND $3::text IS NOT NULL",
				"INSERT INTO photo.v2_qualification_policies(id,operation,operation_version,model_lock_sha,runtime_lock_sha,backend,device_profile_sha,dataset_sha,issuer_id,required_checks,test_fixture_shas,calibration_shas,evidence_shas,license_cleared,enabled,admission_enabled) SELECT $2,$3,operation_version,model_lock_sha,runtime_lock_sha,backend,device_profile_sha,dataset_sha,issuer_id,required_checks,test_fixture_shas,calibration_shas,evidence_shas,license_cleared,enabled,admission_enabled FROM photo.v2_qualification_policies WHERE id=$1",
			}
			for _, statement := range policyStatements {
				writer, err := evidenceOwner.Begin(ctx)
				if err != nil {
					policyBarrierTx.Rollback(context.Background())
					t.Fatal(err)
				}
				if _, err = writer.Exec(ctx, "SET LOCAL lock_timeout='250ms'"); err != nil {
					writer.Rollback(context.Background())
					policyBarrierTx.Rollback(context.Background())
					t.Fatal(err)
				}
				_, writeErr := writer.Exec(ctx, statement, fixture.PersonQualification.PolicyID, uuid.New(), uuid.NewString())
				writer.Rollback(context.Background())
				var denied *pgconn.PgError
				if !errors.As(writeErr, &denied) || denied.Code != "55P03" {
					policyBarrierTx.Rollback(context.Background())
					t.Fatal("global policy writer crossed barrier", writeErr)
				}
			}
			if err := policyBarrierTx.Rollback(context.Background()); err != nil {
				t.Fatal(err)
			}
			for _, statement := range policyStatements {
				writer, err := evidenceOwner.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = writer.Exec(ctx, "SET LOCAL lock_timeout='250ms'"); err != nil {
					writer.Rollback(context.Background())
					t.Fatal(err)
				}
				written, writeErr := writer.Exec(ctx, statement, fixture.PersonQualification.PolicyID, uuid.New(), uuid.NewString())
				writer.Rollback(context.Background())
				if writeErr != nil || written.RowsAffected() != 1 {
					t.Fatal("global policy writer failed after release", writeErr, written.RowsAffected())
				}
			}
			t.Log("V27_GLOBAL_POLICY_PHANTOM_AND_RELEASE_OK")
			t.Log("V27_GLOBAL_QUALIFICATION_POLICY_BARRIER_OK")
			checkPolicySnapshot := func() (bool, error) {
				tx, err := manualLockPool.Begin(ctx)
				if err != nil {
					return false, err
				}
				defer tx.Rollback(context.Background())
				if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", fixture.Tenant.String()); err != nil {
					return false, err
				}
				var accepted bool
				err = tx.QueryRow(ctx, "SELECT photo.lock_semantic_proposal_qualification_policy_snapshot($1,$2,$3,$4,$5)", fixture.Tenant, reader.ProjectID, who.UserID, who.SessionID, policyInventory).Scan(&accepted)
				return accepted, err
			}
			if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=$1", fixture.PersonQualification.PolicyID); err != nil {
				t.Fatal(err)
			}
			if accepted, err := checkPolicySnapshot(); err != nil || accepted {
				t.Fatal("stale nonempty global snapshot accepted", err)
			}
			if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.v2_qualification_policies SET admission_enabled=true WHERE id=$1", fixture.PersonQualification.PolicyID); err != nil {
				t.Fatal(err)
			}
			if accepted, err := checkPolicySnapshot(); err != nil || !accepted {
				t.Fatal("restored original global snapshot rejected", err)
			}
			t.Log("V27_GLOBAL_POLICY_STALE_SNAPSHOT_OK")
			retainedPolicy, err := reader.readQualificationPolicySnapshot(ctx, who, fixture.Tenant)
			if err != nil {
				t.Fatal(err)
			}
			var retainedPersonExpected v2qualification.Expected
			if err := json.Unmarshal([]byte(retainedCoverageProof.Expected), &retainedPersonExpected); err != nil {
				t.Fatal(err)
			}
			selectedPolicies, err := selectQualificationPolicies(retainedPolicy, []string{"mask.person"})
			if err != nil || len(selectedPolicies) != 1 || selectedPolicies["mask.person"].ModelLockSHA != retainedPersonExpected.ModelLockSHA || selectedPolicies["mask.person"].DatasetSHA != retainedPersonExpected.DatasetSHA || selectedPolicies["mask.person"].IssuerID != retainedPersonExpected.IssuerID {
				t.Fatal("server required person policy selection", err)
			}
			t.Log("V27_REQUIRED_POLICY_SELECTION_OK")
			if !sameQualificationPolicyExpected(selectedPolicies["mask.person"], retainedPersonExpected) {
				t.Fatal("full selected person Expected differs")
			}
			if err := matchSelectedEvidencePolicies(retainedPolicy, inventoryProofs, []faceParsePreparationProof{retainedParseProof}, []personProtectionProof{retainedCoverageProof}); err != nil {
				t.Fatal("selected evidence DBA policy matching", err)
			}
			t.Log("V27_SELECTED_EVIDENCE_POLICY_MATCH_OK")
			policyFamilyTx, err := manualLockPool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = policyFamilyTx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", fixture.Tenant.String()); err != nil {
				policyFamilyTx.Rollback(context.Background())
				t.Fatal(err)
			}
			if err := reader.lockExecutionQualifiedSourceInventory(ctx, policyFamilyTx, who, fixture.Tenant, request.Intent.Reference, color, retainedSourceProof, inventoryProofs, []faceParsePreparationProof{retainedParseProof}, []manualPreparationProof{retainedManualProof}, []personProtectionProof{retainedCoverageProof}, retainedPolicy, originalDefect, retainedTransfer, defectExpected, trusted); err != nil {
				policyFamilyTx.Rollback(context.Background())
				t.Fatal("policy shared evidence composer", err)
			}
			policyFamilyTx.Rollback(context.Background())
			t.Log("V27_POLICY_SHARED_SOURCE_INVENTORY_LOCK_OK")
			t.Log("V27_TRANSFER_QUALIFIED_SHARED_SOURCE_LOCK_OK")
			t.Log("V27_EXECUTION_QUALIFIED_SHARED_SOURCE_LOCK_OK")
			foreignPolicyExpected := retainedPersonExpected
			foreignPolicyExpected.RuntimeLockSHA = strings.Repeat("0", 64)
			foreignPolicyBody, err := canonical.Marshal(foreignPolicyExpected)
			if err != nil {
				t.Fatal(err)
			}
			foreignPolicyProof := retainedCoverageProof
			foreignPolicyProof.Expected = string(foreignPolicyBody)
			if err := reader.lockPolicySelectedSourceInventory(ctx, nil, who, fixture.Tenant, request.Intent.Reference, color, retainedSourceProof, inventoryProofs, []faceParsePreparationProof{retainedParseProof}, []manualPreparationProof{retainedManualProof}, []personProtectionProof{foreignPolicyProof}, retainedPolicy); err != v2qualification.ErrUnqualified {
				t.Fatal("foreign active policy was not denied before SQL", err)
			}
			t.Log("V27_REQUIRED_POLICY_PREFLIGHT_DENIAL_OK")

			lockRetainedManual := func(proof manualPreparationProof) error {
				tx, err := manualLockPool.Begin(ctx)
				if err != nil {
					return err
				}
				defer tx.Rollback(context.Background())
				if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", fixture.Tenant.String()); err != nil {
					return err
				}
				return reader.lockManualPreparationProof(ctx, tx, who, fixture.Tenant, request.Intent.Reference, proof)
			}
			if err := lockRetainedManual(retainedManualProof); err != nil {
				t.Fatal("original-content retained manual transaction lock", err)
			}
			if fixture.SecondManualTarget != nil {
				_, secondProof, err := reader.readManualPreparationProof(ctx, who, fixture.Tenant, request.Intent.Reference, *fixture.SecondManualTarget)
				if err != nil {
					t.Fatal("second original manual proof", err)
				}
				if secondProof.objects.Root.ref.ArtifactID == retainedManualProof.objects.Root.ref.ArtifactID {
					t.Fatal("fixture reused manual root")
				}
				lockBatch := func(proofs []manualPreparationProof) error {
					tx, err := manualLockPool.Begin(ctx)
					if err != nil {
						return err
					}
					defer tx.Rollback(context.Background())
					if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", fixture.Tenant.String()); err != nil {
						return err
					}
					return reader.lockManualPreparationProofs(ctx, tx, who, fixture.Tenant, request.Intent.Reference, proofs)
				}
				if err := lockBatch([]manualPreparationProof{secondProof, retainedManualProof, secondProof}); err != nil {
					t.Fatal("two original manual roots batch", err)
				}
				changedSecond := secondProof
				changedSecond.objects.Params.object.Key += "-changed"
				if err := lockBatch([]manualPreparationProof{retainedManualProof, changedSecond}); !errors.Is(err, v2input.ErrConflict) {
					t.Fatal("changed second manual batch accepted", err)
				}
				if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1", secondProof.objects.Root.ref.ArtifactID); err != nil {
					t.Fatal(err)
				}
				if err := lockBatch([]manualPreparationProof{retainedManualProof, secondProof}); !errors.Is(err, v2input.ErrConflict) {
					t.Fatal("expired second manual batch accepted", err)
				}
				if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=NULL WHERE id=$1", secondProof.objects.Root.ref.ArtifactID); err != nil {
					t.Fatal(err)
				}
				if err := lockBatch([]manualPreparationProof{retainedManualProof, secondProof}); err != nil {
					t.Fatal("restored manual batch", err)
				}
				t.Log("V27_MANUAL_MULTI_ROOT_LOCK_OK")
			}
			changedManualProof := retainedManualProof
			changedManualProof.objects.Root.object.Key += "-changed"
			if err := lockRetainedManual(changedManualProof); !errors.Is(err, v2input.ErrConflict) {
				t.Fatal("changed retained manual location accepted", err)
			}
			if (retainedManualProof.vectors != nil) != (fixture.ManualVectors != nil) {
				t.Fatal("external vectors missing from proof")
			}
			if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=NULL WHERE id=$1", fixture.ManualFaceDefectPins.Report.ArtifactID); err != nil {
				t.Fatal(err)
			}
			work, err := preparedManual.Raster.EstimateStreamWork()
			if err != nil {
				t.Fatal(err)
			}
			for _, limits := range []ManualRasterVerificationLimits{{}, {PointChecks: 10000000, EdgeEvaluations: 100000000, PayloadBytes: work.NPYPayloadBytes - 1}} {
				spy := &corruptDefectOpen{delegate: objects, key: preProducerObjects.Pixel.object.Key}
				limitedReader := reader
				limitedReader.Objects = spy
				limitedReader.ManualRasterLimits = limits
				if denied, err := limitedReader.ReadManualPreparation(ctx, who, fixture.Tenant, request.Intent.Reference, manualRoots[0].Binding.Mask); err == nil || denied.Body != nil || spy.done {
					t.Fatal("manual raster budget did not refuse before NPY IO", err)
				}
				if denied, err := limitedReader.ReadDefectFaceMaskRoots(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.ManualFaceDefectPins); err == nil || denied != nil || spy.done {
					t.Fatal("finished manual raster budget did not refuse before NPY IO", err)
				}
			}

			if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1", preProducerObjects.Root.ref.ArtifactID); err != nil {
				t.Fatal(err)
			}
			if err := lockRetainedManual(retainedManualProof); !errors.Is(err, v2input.ErrConflict) {
				t.Fatal("expired retained manual transaction proof accepted", err)
			}
			if err := reader.recheckManualPreparationProof(ctx, who, fixture.Tenant, request.Intent.Reference, retainedManualProof); err == nil {
				t.Fatal("stale retained manual proof accepted")
			}
			deniedManualObjects, manualOriginErr := reader.manualPreparationObjects(ctx, who, fixture.Tenant, request.Intent.Reference, v2input.Ref{ArtifactID: actualManualPixel.ID, SHA256: actualManualPixel.SHA256})
			if manualOriginErr == nil || deniedManualObjects != (defectMaskObjects{}) {
				t.Fatal("expired pre-producer root accepted")
			}
			if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=NULL WHERE id=$1", preProducerObjects.Root.ref.ArtifactID); err != nil {
				t.Fatal(err)
			}
			if fixture.ManualLockOnly {
				if err := lockRetainedManual(retainedManualProof); err != nil {
					t.Fatal("restored retained manual proof rejected", err)
				}
				t.Log("V27_MANUAL_RETAINED_TRANSACTION_LOCK_OK")
				if deadline, ok := ctx.Deadline(); ok {
					t.Logf("V27_FIRST_STAGE_REMAINING_SECONDS %.3f", time.Until(deadline).Seconds())
				}
				// This capability change is confined to the disposable database.
				// Committed admission is observed from a different SQL connection.
				if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.operation_registry SET v2_enabled=true WHERE operation='defects.propose' AND version='1'"); err != nil {
					t.Fatal(err)
				}
				defer evidenceOwner.Exec(context.Background(), "UPDATE photo.operation_registry SET v2_enabled=false WHERE operation='defects.propose' AND version='1'")
				selectedOrigin, err := reader.discoverDefectOriginForProducer(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.ManualFaceDefectTask, fixture.ManualFaceDefectPins.Params.SHA256)
				if err != nil || selectedOrigin != fixture.ManualFaceDefectPins {
					t.Fatal("exact Params producer discovery", err)
				}
				missingOrigin, missingErr := reader.discoverDefectOriginForProducer(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.ManualFaceDefectTask, strings.Repeat("0", 64))
				if missingErr == nil || missingOrigin != (DefectOriginPins{}) {
					t.Fatal("missing Params producer returned origin", missingErr)
				}
				wrongProducer, wrongProducerErr := reader.discoverDefectOriginForProducer(ctx, who, fixture.Tenant, request.Intent.Reference, uuid.New(), fixture.ManualFaceDefectPins.Params.SHA256)
				if wrongProducerErr == nil || wrongProducer != (DefectOriginPins{}) {
					t.Fatal("wrong producer returned origin", wrongProducerErr)
				}
				wrongSource := request.Intent.Reference
				wrongSource.Source.ArtifactID = uuid.New()
				foreignOrigin, foreignErr := reader.discoverDefectOriginForProducer(ctx, who, fixture.Tenant, wrongSource, fixture.ManualFaceDefectTask, fixture.ManualFaceDefectPins.Params.SHA256)
				if foreignErr == nil || foreignOrigin != (DefectOriginPins{}) {
					t.Fatal("wrong source returned original producer", foreignErr)
				}
				stagePreparer := TransferDraftPreparer{Reader: reader, Documents: publisher, Scheduler: manualLockPool, TransportDocuments: defectTransportPublisher.Documents, Configuration: TransferPreparation{Color: color}}
				stagePreparation := originFirstDefectPreparation{producerID: fixture.ManualFaceDefectTask, paramsSHA: fixture.ManualFaceDefectPins.Params.SHA256, person: retainedCoverageProof, costs: defectCosts, ram: 1 << 40, vram: 0, scratch: 1 << 40, output: 1 << 40, reserved: 64 << 20}
				stageStarted := time.Now()
				committedTicket, err := stagePreparer.prepareAndAdmitOriginDefectStage(ctx, who, request, "proposal-publication", first, snapshotRef, snapshotBody, stagePreparation)
				t.Logf("V27_FIRST_STAGE_DURATION_SECONDS %.3f", time.Since(stageStarted).Seconds())
				if err != nil {
					t.Fatal("committed native admission", err)
				}
				replayedTicket, err := reader.sealRegisteredDefectPreparation(ctx, defectResumePool, who, defectPersonAdmission, defectTransport.Graph.Built)
				if err != nil || replayedTicket != committedTicket {
					t.Fatal("committed native admission replay", err)
				}
				readCompletedState := func() string {
					var state string
					if err := evidenceOwner.QueryRow(ctx, `SELECT jsonb_build_object(
                        'run',jsonb_build_object('status',r.status,'revision',r.revision,'sealed',r.graph_sealed,'cancel',r.cancel_requested),
                        'tasks',(SELECT jsonb_agg(jsonb_build_object('id',t.id,'state',t.state,'attempt',t.attempt,'inputs',t.inputs,'profile',t.execution_profile) ORDER BY t.id) FROM photo.tasks t WHERE t.tenant_id=r.tenant_id AND t.run_id=r.id),
                        'inputs',(SELECT jsonb_agg(jsonb_build_object('task',i.task_id,'ordinal',i.ordinal,'artifact',i.artifact_id,'sha',i.sha256,'contract',i.contract) ORDER BY i.task_id,i.ordinal) FROM photo.v2_task_inputs i JOIN photo.tasks t ON t.tenant_id=i.tenant_id AND t.id=i.task_id WHERE t.tenant_id=r.tenant_id AND t.run_id=r.id),
                        'events',(SELECT count(*) FROM photo.run_events e WHERE e.tenant_id=r.tenant_id AND e.run_id=r.id),
                        'outbox',(SELECT count(*) FROM photo.outbox o WHERE o.tenant_id=r.tenant_id AND o.payload->>'run_id'=r.id::text),
                        'receipt',(SELECT jsonb_agg(jsonb_build_object('status',i.response_status,'body',i.response_body) ORDER BY i.id) FROM photo.idempotency_records i WHERE i.tenant_id=r.tenant_id AND i.resource_id=r.id)
                        )::text FROM photo.runs r WHERE r.tenant_id=$1 AND r.id=$2`, fixture.Tenant, committedTicket.RunID).Scan(&state); err != nil {
						t.Fatal("completed preparation state observer", err)
					}
					return state
				}
				beforeCompletedDenials := readCompletedState()
				for replay := 0; replay < 2; replay++ {
					recoveryPreparer := stagePreparer
					recoveryPreparer.Scheduler = defectResumePool
					restartPreparation := stagePreparation
					restartPreparation.producerID = uuid.Nil
					restartPreparation.paramsSHA = ""
					recovered, err := recoveryPreparer.prepareAndAdmitOriginDefectStage(ctx, who, request, "proposal-publication", first, snapshotRef, snapshotBody, restartPreparation)
					if err != nil || recovered != committedTicket {
						t.Fatal("completed native preparation transport recovery", replay, recovered, err)
					}
				}
				t.Log("V27_DEFECT_NATIVE_COMPLETED_TRANSPORT_RECOVERY_OK")
				t.Log("V27_DEFECT_FIRST_STAGE_ORCHESTRATOR_OK")
				t.Log("V27_DEFECT_ORIGIN_FIRST_STAGE_OK")
				t.Log("V27_DEFECT_EXACT_PRODUCER_DISCOVERY_STAGE_OK")
				changedClosure := defectTransport.Graph.Built
				changedClosure.Manifest.ArtifactID = uuid.New()
				deniedTicket, deniedErr := reader.sealRegisteredDefectPreparation(ctx, defectResumePool, who, defectPersonAdmission, changedClosure)
				if deniedErr == nil || deniedTicket != (v2input.JobTicket{}) {
					t.Fatal("completed native preparation accepted changed closure", deniedTicket, deniedErr)
				}
				if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE tenant_id=$1 AND id=$2", fixture.Tenant, retainedCoverageProof.Protection.ArtifactID); err != nil {
					t.Fatal(err)
				}
				deniedTicket, deniedErr = reader.sealRegisteredDefectPreparation(ctx, defectResumePool, who, defectPersonAdmission, defectTransport.Graph.Built)
				if deniedErr == nil || deniedTicket != (v2input.JobTicket{}) {
					t.Fatal("completed native preparation accepted expired person protection", deniedTicket, deniedErr)
				}
				if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=NULL WHERE tenant_id=$1 AND id=$2", fixture.Tenant, retainedCoverageProof.Protection.ArtifactID); err != nil {
					t.Fatal(err)
				}
				restoredTicket, err := reader.sealRegisteredDefectPreparation(ctx, defectResumePool, who, defectPersonAdmission, defectTransport.Graph.Built)
				if err != nil || restoredTicket != committedTicket {
					t.Fatal("completed native preparation failed after authority restore", restoredTicket, err)
				}
				if after := readCompletedState(); after != beforeCompletedDenials {
					t.Fatal("completed preparation replay mutated graph, receipt or events", beforeCompletedDenials, after)
				}
				t.Log("V27_DEFECT_NATIVE_COMPLETED_READ_ONLY_OK")
				t.Log("V27_DEFECT_NATIVE_COMPLETED_AUTHORITY_OK")
				var observedRun, observedTask string
				var observedSealed bool
				if err := evidenceOwner.QueryRow(ctx, "SELECT r.status,r.graph_sealed,t.state FROM photo.runs r JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id WHERE r.tenant_id=$1 AND r.id=$2", fixture.Tenant, committedTicket.RunID).Scan(&observedRun, &observedSealed, &observedTask); err != nil || observedRun != "queued" || !observedSealed || observedTask != "ready" {
					t.Fatal("committed native state", observedRun, observedTask, err)
				}
				t.Log("V27_DEFECT_NATIVE_PENDING_COORDINATOR_OK")
				t.Log("V27_DEFECT_NATIVE_COMMITTED_ADMISSION_OK")
				return
			}
			if !fixture.ManualRaceOnly {
				manualRootBody, err := v2input.ReadPinnedSemanticDocument(ctx, objects, actualManualObjects.Root.object, actualManualObjects.Root.ref)
				if err != nil {
					t.Fatal(err)
				}
				manualSourceBody, err := reader.Read(ctx, who, fixture.Tenant, request.Intent.Reference)
				if err != nil {
					t.Fatal(err)
				}
				manualImage, err := review.ParseWorkingImage(manualSourceBody)
				if err != nil {
					t.Fatal(err)
				}
				manualSource := manualImage.Source(review.ArtifactRef{ID: request.Intent.Reference.Source.ArtifactID, SHA256: request.Intent.Reference.Source.SHA256})
				manualEvidence, err := reader.ReadDefectEvidence(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.ManualFaceDefectPins)
				if err != nil {
					t.Fatal(err)
				}
				originalProtections, err := review.ProposalDeclaredProtections(manualEvidence.Report, review.ArtifactRef{ID: fixture.ManualFaceDefectPins.Proposal.ArtifactID, SHA256: fixture.ManualFaceDefectPins.Proposal.SHA256}, manualSource)
				if err != nil || len(originalProtections) != 2 {
					t.Fatal("original preparation protections", err)
				}
				var originalManualParams struct {
					Faces    []review.DefectProposalFaceInput `json:"faces"`
					Diameter [2]float64                       `json:"diameter_face_fraction"`
				}
				if err := json.Unmarshal(manualEvidence.Params, &originalManualParams); err != nil {
					t.Fatal(err)
				}
				automaticManualTask, err := reader.PrepareDefectManualTaskInputs(ctx, who, fixture.Tenant, request.Intent.Reference, color, originalManualParams.Faces, originalManualParams.Diameter, &originalProtections[0], nil)
				if err != nil || !bytes.Equal(automaticManualTask.Params, manualEvidence.Params) || len(automaticManualTask.ManualRoots) != 1 || automaticManualTask.ManualRoots[0] != manualRoots[0].Root {
					t.Fatal("automatic manual task preparation changed original Params", err)
				}
				preparedManualParams, err := reader.prepareManualDefectParams(ctx, who, fixture.Tenant, request.Intent.Reference, color, originalManualParams.Faces, originalManualParams.Diameter, &originalProtections[0], nil)
				if err != nil || preparedManualParams.manualCount != 1 || preparedManualParams.params.params != string(automaticManualTask.Params) || preparedManualParams.params.digest != automaticManualTask.ParamsSHA256 || preparedManualParams.manual[0].proof.objects.Root.ref.ArtifactID != manualRoots[0].Root.ID {
					t.Fatal("immutable manual Params preparation lost original evidence", err)
				}
				t.Log("V27_DEFECT_MANUAL_PARAMS_RETAINED_OK")
				aggregateReader := reader
				aggregateWork, err := preparedManual.Raster.EstimateStreamWork()
				if err != nil {
					t.Fatal(err)
				}
				aggregateReader.ManualRasterBatchLimits.PayloadBytes = aggregateWork.NPYPayloadBytes
				if denied, err := aggregateReader.PrepareDefectManualTaskInputs(ctx, who, fixture.Tenant, request.Intent.Reference, color, originalManualParams.Faces, originalManualParams.Diameter, &originalProtections[0], nil); err == nil || denied.Params != nil || denied.Inputs != nil {
					t.Fatal("repeated manual pass reset aggregate budget", err)
				}
				pixelDeclared := false
				for _, input := range automaticManualTask.Inputs {
					if input == manualImage.Pixels {
						pixelDeclared = true
					}
				}
				if !pixelDeclared {
					t.Fatal("automatic manual task omitted original RGB")
				}
				if denied, err := reader.PrepareDefectManualTaskInputs(ctx, who, fixture.Tenant, request.Intent.Reference, TransferColorProfile{}, originalManualParams.Faces, originalManualParams.Diameter, &originalProtections[0], nil); err == nil || denied.Params != nil || denied.Inputs != nil {
					t.Fatal("unqualified source profile prepared")
				}
				compiledProtection, err := review.BuildDefectProtectionInputs(ctx, manualSource, &originalProtections[0], nil, []review.OriginalManualProtection{{Root: manualRoots[0].Root, Body: manualRootBody, Target: manualRoots[0].Binding.Mask}})
				if err != nil || len(compiledProtection.Protections) != 1 || len(compiledProtection.Inputs) != 2 || len(compiledProtection.Roots) != 1 || compiledProtection.Protections[0].Artifact.ID != fixture.ManualOutputs[0].ArtifactID {
					t.Fatal("automatic original manual protection preparation", err)
				}
				publicationConfig, err := pgxpool.ParseConfig(os.Getenv("PHOTO_TEST_DATABASE_URL"))
				if err != nil {
					t.Fatal("defect publication database configuration")
				}
				publicationConfig.ConnConfig.RuntimeParams["role"] = "photo_scheduler"
				publicationDB, err := pgxpool.NewWithConfig(ctx, publicationConfig)
				if err != nil {
					t.Fatal("defect publication database unavailable")
				}
				defer publicationDB.Close()
				var publicationRun uuid.UUID
				if err := evidenceOwner.QueryRow(ctx, "SELECT run_id FROM photo.tasks WHERE tenant_id=$1 AND id=$2", fixture.Tenant, declarationProducer).Scan(&publicationRun); err != nil {
					t.Fatal(err)
				}
				publicationScope := artifactjson.Scope{Tenant: fixture.Tenant, Run: publicationRun, Owner: uuid.New(), Purpose: "task_params"}
				putCalled := false
				defectPublisher := artifactjson.Publisher{Repository: artifactjson.SQLRepository{DB: publicationDB, Bucket: fixture.Objects.Bucket}, Objects: afterIntentPut{ImmutableStore: objects, after: func() error { putCalled = true; return nil }}}
				deniedPublication, publicationErr := CompileAndPublishDefectParams(ctx, defectPublisher, publicationScope, manualSource.Image, manualSourceBody, []review.DefectProposalFaceInput{}, [2]float64{.002, .012}, &originalProtections[0], nil, []review.OriginalManualProtection{{Root: manualRoots[0].Root, Body: manualRootBody, Target: manualRoots[0].Binding.Mask}})
				if !errors.Is(publicationErr, artifactjson.ErrStale) || putCalled || deniedPublication.Ref.ArtifactID != uuid.Nil || deniedPublication.Inputs != nil {
					t.Fatal("unregistered defect Params owner published", publicationErr)
				}
				var rejectedDocuments int
				if err := evidenceOwner.QueryRow(ctx, "SELECT count(*) FROM photo.backend_documents WHERE tenant_id=$1 AND run_id=$2 AND owner_id=$3", publicationScope.Tenant, publicationScope.Run, publicationScope.Owner).Scan(&rejectedDocuments); err != nil || rejectedDocuments != 0 {
					t.Fatal("refused owner reserved a document", err)
				}
			}
			manualPixelRef := v2input.Ref{ArtifactID: actualManualPixel.ID, SHA256: actualManualPixel.SHA256}
			manualChildren, err := reader.defectManualOutputObjects(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.ManualFaceDefectPins, manualPixelRef, fixture.ManualOutputs)
			if err != nil {
				t.Fatal("canonical manual children", err)
			}
			preProducerChildren, err := reader.manualPreparationOutputObjects(ctx, who, fixture.Tenant, request.Intent.Reference, manualPixelRef, fixture.ManualOutputs)
			if err != nil || preProducerChildren != manualChildren {
				t.Fatal("pre-producer manual output catalog mismatch", err)
			}
			if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1", fixture.ManualFaceDefectPins.Report.ArtifactID); err != nil {
				t.Fatal(err)
			}
			independentChildren, independentErr := reader.manualPreparationOutputObjects(ctx, who, fixture.Tenant, request.Intent.Reference, manualPixelRef, fixture.ManualOutputs)
			if independentErr != nil || independentChildren != preProducerChildren {
				t.Fatal("manual outputs depend on defect report", independentErr)
			}
			if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=NULL WHERE id=$1", fixture.ManualFaceDefectPins.Report.ArtifactID); err != nil {
				t.Fatal(err)
			}
			wrongRoles := fixture.ManualOutputs
			wrongRoles[1], wrongRoles[2] = wrongRoles[2], wrongRoles[1]
			if denied, err := reader.manualPreparationOutputObjects(ctx, who, fixture.Tenant, request.Intent.Reference, manualPixelRef, wrongRoles); err == nil || denied != (defectManualOutputs{}) {
				t.Fatal("pre-producer PNG role swap accepted", err)
			}
			for _, child := range preProducerChildren {
				if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1", child.ref.ArtifactID); err != nil {
					t.Fatal(err)
				}
				if denied, err := reader.manualPreparationOutputObjects(ctx, who, fixture.Tenant, request.Intent.Reference, manualPixelRef, fixture.ManualOutputs); err == nil || denied != (defectManualOutputs{}) {
					t.Fatal("expired pre-producer child accepted", err)
				}
				if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=NULL WHERE id=$1", child.ref.ArtifactID); err != nil {
					t.Fatal(err)
				}
			}
			if denied, err := reader.defectManualOutputObjects(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.ManualFaceDefectPins, manualPixelRef, wrongRoles); err == nil || denied != (defectManualOutputs{}) {
				t.Fatal("manual PNG role swap accepted", err)
			}
			for _, child := range preProducerChildren {
				corrupt := &corruptDefectOpen{delegate: objects, key: child.object.Key}
				preparationReader := reader
				preparationReader.Objects = corrupt
				denied, err := preparationReader.ReadManualPreparation(ctx, who, fixture.Tenant, request.Intent.Reference, manualRoots[0].Binding.Mask)
				if err == nil || denied.Root.ID != uuid.Nil || denied.Body != nil || !corrupt.done {
					t.Fatal("corrupt pre-producer manual content accepted", err)
				}
			}
			preparationRace := &afterSecondMaskOpen{delegate: objects, key: preProducerChildren[2].object.Key, opens: 1, after: func(ctx context.Context) error {
				_, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1", preProducerObjects.Root.ref.ArtifactID)
				return err
			}}
			preparationReader := reader
			preparationReader.Objects = preparationRace
			if denied, err := preparationReader.ReadManualPreparation(ctx, who, fixture.Tenant, request.Intent.Reference, manualRoots[0].Binding.Mask); err == nil || denied.Body != nil || !preparationRace.done {
				t.Fatal("stale manual root after binary IO accepted", err)
			}
			if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=NULL WHERE id=$1", preProducerObjects.Root.ref.ArtifactID); err != nil {
				t.Fatal(err)
			}
			for _, child := range manualChildren {
				corrupt := &corruptDefectOpen{delegate: objects, key: child.object.Key}
				corruptReader := reader
				corruptReader.Objects = corrupt
				denied, err := corruptReader.ReadDefectFaceMaskRoots(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.ManualFaceDefectPins)
				if err == nil || denied != nil || !corrupt.done {
					t.Fatal("corrupt manual output accepted", err)
				}
			}
			for _, child := range manualChildren {
				race := &afterSecondMaskOpen{delegate: objects, key: manualChildren[1].object.Key, opens: 1, after: func(ctx context.Context) error {
					_, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 day' WHERE id=$1", child.ref.ArtifactID)
					return err
				}}
				raceReader := reader
				raceReader.Objects = race
				denied, err := raceReader.ReadDefectFaceMaskRoots(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.ManualFaceDefectPins)
				if err == nil || denied != nil || !race.done {
					t.Fatal("manual child expiry during IO accepted", err)
				}
				if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=NULL WHERE id=$1", child.ref.ArtifactID); err != nil {
					t.Fatal(err)
				}
			}
			if restored, err := reader.ReadDefectFaceMaskRoots(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.ManualFaceDefectPins); err != nil || len(restored) != 1 {
				t.Fatal("restored full manual output inventory", err)
			}
			falseSkinObjects, falseSkinErr := reader.defectSkinObjects(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.ManualFaceDefectPins, v2input.Ref{ArtifactID: actualManualPixel.ID, SHA256: actualManualPixel.SHA256}, fixture.ManualFaceMaskRoot)
			if falseSkinErr != v2input.ErrInvalid || falseSkinObjects != (defectSkinParseObjects{}) {
				t.Fatal("manual root accepted as parse authority", falseSkinErr)
			}
			if fixture.ManualVectors != nil {
				var manualProducer uuid.UUID
				var originalInputs json.RawMessage
				if err := evidenceOwner.QueryRow(ctx, "SELECT task.id,task.inputs FROM photo.tasks task JOIN photo.artifacts pixel ON pixel.tenant_id=task.tenant_id AND pixel.created_by_task_id=task.id WHERE pixel.id=$1", actualManualPixel.ID).Scan(&manualProducer, &originalInputs); err != nil {
					t.Fatal(err)
				}
				if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.tasks SET inputs=jsonb_build_array(jsonb_build_object('artifact_id',$2::uuid,'sha256',$3::text)) WHERE id=$1", manualProducer, request.Intent.Reference.Source.ArtifactID, request.Intent.Reference.Source.SHA256); err != nil {
					t.Fatal(err)
				}
				undeclared, undeclaredErr := reader.ReadDefectFaceMaskRoots(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.ManualFaceDefectPins)
				if undeclaredErr != v2input.ErrInvalid || undeclared != nil {
					t.Fatal("undeclared external vectors accepted", undeclaredErr)
				}
				if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.tasks SET inputs=$2::jsonb WHERE id=$1", manualProducer, originalInputs); err != nil {
					t.Fatal(err)
				}
				if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 day' WHERE id=$1", fixture.ManualVectors.ArtifactID); err != nil {
					t.Fatal(err)
				}
				denied, err := reader.ReadDefectFaceMaskRoots(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.ManualFaceDefectPins)
				if err != v2input.ErrInvalid || denied != nil {
					t.Fatal("expired external vectors accepted", err)
				}
				if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=NULL WHERE id=$1", fixture.ManualVectors.ArtifactID); err != nil {
					t.Fatal(err)
				}
			}
			corruptManual := &corruptDefectOpen{delegate: objects, key: actualManualObjects.Pixel.object.Key}
			corruptManualReader := reader
			corruptManualReader.Objects = corruptManual
			corruptManualRoots, corruptManualErr := corruptManualReader.ReadDefectFaceMaskRoots(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.ManualFaceDefectPins)
			if corruptManualErr == nil || corruptManualRoots != nil || !corruptManual.done {
				t.Fatal("corrupt rasterized manual pixels accepted", corruptManualErr)
			}
			if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 day' WHERE id=$1", fixture.ManualFaceMaskRoot.ArtifactID); err != nil {
				t.Fatal(err)
			}
			expiredRoots, expiredRootsErr := reader.ReadDefectFaceMaskRoots(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.ManualFaceDefectPins)
			if expiredRootsErr != v2input.ErrInvalid || expiredRoots != nil {
				t.Fatal("expired manual root accepted", expiredRootsErr)
			}
			if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=NULL WHERE id=$1", fixture.ManualFaceMaskRoot.ArtifactID); err != nil {
				t.Fatal(err)
			}
		}
		if fixture.ManualRaceOnly {
			t.Log("V27_MANUAL_IO_RACES_OK")
			return
		}
		skinRoots, skinErr := reader.ReadDefectFaceMaskRoots(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.SkinFaceDefectPins)
		if skinErr != nil || len(skinRoots) != 1 || skinRoots[0].Root.ID != fixture.SkinMaskRoot.ArtifactID || skinRoots[0].Root.SHA256 != fixture.SkinMaskRoot.SHA256 || skinRoots[0].Binding.Mask.Confidence != .9 {
			t.Fatal("canonical skin source chain and physical pixels", skinErr)
		}
		skinProfileExpected := [2]v2qualification.Expected{parseExpected, qualifiedProfiles[1]}
		skinPixelRef := v2input.Ref{ArtifactID: skinRoots[0].Binding.Mask.Artifact.ID, SHA256: skinRoots[0].Binding.Mask.Artifact.SHA256}
		if err := reader.VerifyDefectSkinProducerProfiles(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.SkinFaceDefectPins, skinPixelRef, fixture.SkinParse, fixture.DefectDetection, skinProfileExpected); err != nil {
			t.Fatal("skin original producer locks", err)
		}
		skinReports := [2]v2input.Ref{fixture.ParseQualification.Report, faceQualificationReports[1]}
		qualifiedSkin, qualifiedSkinErr := reader.ReadQualifiedDefectFaceMaskRoots(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.SkinFaceDefectPins, skinReports, skinProfileExpected, color)
		if fixture.OrientedSource || fixture.CreatePerson {
			if qualifiedSkinErr != nil || len(qualifiedSkin) != 1 {
				t.Fatal("complete qualified skin inventory", qualifiedSkinErr)
			}
		} else if qualifiedSkinErr == nil || qualifiedSkin != nil {
			t.Fatal("HDR source accepted by qualified skin reader", qualifiedSkinErr)
		}
		for _, invalidColor := range []TransferColorProfile{{}, {sha: strings.Repeat("0", 64)}} {
			if denied, err := reader.ReadQualifiedDefectFaceMaskRoots(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.SkinFaceDefectPins, skinReports, skinProfileExpected, invalidColor); err == nil || denied != nil {
				t.Fatal("unqualified ICC accepted by skin reader", err)
			}
		}
		wrongSkinReports := skinReports
		wrongSkinReports[0] = faceQualificationReports[0]
		if denied, err := reader.ReadQualifiedDefectFaceMaskRoots(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.SkinFaceDefectPins, wrongSkinReports, skinProfileExpected, color); err != v2qualification.ErrUnqualified || denied != nil {
			t.Fatal("wrong qualification for complete skin inventory", err)
		}
		var skinParser uuid.UUID
		if err := evidenceOwner.QueryRow(ctx, "SELECT created_by_task_id FROM photo.artifacts WHERE id=$1", fixture.SkinParse.ArtifactID).Scan(&skinParser); err != nil {
			t.Fatal(err)
		}
		if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.task_attempts SET profile=jsonb_set(profile,'{runtime_lock_sha}',to_jsonb($2::text)) WHERE task_id=$1", skinParser, strings.Repeat("d", 64)); err != nil {
			t.Fatal(err)
		}
		if err := reader.VerifyDefectSkinProducerProfiles(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.SkinFaceDefectPins, skinPixelRef, fixture.SkinParse, fixture.DefectDetection, skinProfileExpected); err != v2qualification.ErrUnqualified {
			t.Fatal("changed parser runtime accepted", err)
		}
		if denied, err := reader.ReadQualifiedDefectFaceMaskRoots(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.SkinFaceDefectPins, skinReports, skinProfileExpected, color); err != v2qualification.ErrUnqualified || denied != nil {
			t.Fatal("changed parser runtime accepted by full reader", err)
		}
		if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.task_attempts SET profile=jsonb_set(profile,'{runtime_lock_sha}',to_jsonb($2::text)) WHERE task_id=$1", skinParser, parseExpected.RuntimeLockSHA); err != nil {
			t.Fatal(err)
		}
		for _, ref := range []v2input.Ref{fixture.SkinPNG, fixture.SkinLabels, {ArtifactID: skinRoots[0].Binding.Mask.Artifact.ID, SHA256: skinRoots[0].Binding.Mask.Artifact.SHA256}} {
			var key string
			if err := evidenceOwner.QueryRow(ctx, "SELECT object_key FROM photo.artifacts WHERE id=$1", ref.ArtifactID).Scan(&key); err != nil {
				t.Fatal(err)
			}
			corrupt := &corruptDefectOpen{delegate: objects, key: key}
			corruptedReader := reader
			corruptedReader.Objects = corrupt
			denied, err := corruptedReader.ReadDefectFaceMaskRoots(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.SkinFaceDefectPins)
			if err == nil || denied != nil || !corrupt.done {
				t.Fatal("corrupted skin chain binary accepted", err)
			}
		}
		var skinLabelKey string
		if err := evidenceOwner.QueryRow(ctx, "SELECT object_key FROM photo.artifacts WHERE id=$1", fixture.SkinLabels.ArtifactID).Scan(&skinLabelKey); err != nil {
			t.Fatal(err)
		}
		if fixture.OrientedSource {
			for _, policyID := range []uuid.UUID{fixture.ParseQualification.PolicyID, fixture.FaceQualifications[1].PolicyID} {
				race := &afterSecondMaskOpen{delegate: objects, key: skinLabelKey, opens: 1, after: func(ctx context.Context) error {
					_, err := evidenceOwner.Exec(ctx, "UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=$1", policyID)
					return err
				}}
				raceReader := reader
				raceReader.Objects = race
				denied, err := raceReader.ReadQualifiedDefectFaceMaskRoots(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.SkinFaceDefectPins, skinReports, skinProfileExpected, color)
				if err != v2qualification.ErrUnqualified || denied != nil || !race.done {
					t.Fatal("qualification policy disabled during skin IO accepted", err)
				}
				if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.v2_qualification_policies SET admission_enabled=true WHERE id=$1", policyID); err != nil {
					t.Fatal(err)
				}
			}
			if restored, err := reader.ReadQualifiedDefectFaceMaskRoots(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.SkinFaceDefectPins, skinReports, skinProfileExpected, color); err != nil || len(restored) != 1 {
				t.Fatal("restored qualified skin policies", err)
			}
		}
		for _, expiredRef := range []v2input.Ref{fixture.SkinPNG, fixture.SkinLabels, fixture.SkinMaskRoot} {
			race := &afterSecondMaskOpen{delegate: objects, key: skinLabelKey, opens: 1, after: func(ctx context.Context) error {
				_, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 day' WHERE id=$1", expiredRef.ArtifactID)
				return err
			}}
			raceReader := reader
			raceReader.Objects = race
			denied, err := raceReader.ReadDefectFaceMaskRoots(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.SkinFaceDefectPins)
			if err == nil || denied != nil || !race.done {
				t.Fatal("skin catalog expiry during pixel IO accepted", err)
			}
			if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=NULL WHERE id=$1", expiredRef.ArtifactID); err != nil {
				t.Fatal(err)
			}
		}
		if restored, err := reader.ReadDefectFaceMaskRoots(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.SkinFaceDefectPins); err != nil || len(restored) != 1 {
			t.Fatal("restored skin inventory", err)
		}
		multiFaces, multiErr := reader.ReadQualifiedDefectFaceSet(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.MultiFaceDefectPins, faceQualificationReports, qualifiedProfiles)
		if multiErr != nil || len(multiFaces) != 2 || multiFaces[0].ObjectID != fixture.DefectFaceID || multiFaces[1].ObjectID != fixture.SecondDefectFaceID {
			t.Fatal("qualified multi-collection set", multiFaces, multiErr)
		}
		var secondTask uuid.UUID
		if err := evidenceOwner.QueryRow(ctx, "SELECT created_by_task_id FROM photo.artifacts WHERE id=$1", fixture.SecondDefectLandmark.ArtifactID).Scan(&secondTask); err != nil {
			t.Fatal(err)
		}
		if _, err := evidenceOwner.Exec(ctx, `UPDATE photo.task_attempts SET profile=jsonb_set(profile,'{runtime_lock_sha}',to_jsonb($2::text)) WHERE task_id=$1`, secondTask, strings.Repeat("d", 64)); err != nil {
			t.Fatal(err)
		}
		multiFaces, multiErr = reader.ReadQualifiedDefectFaceSet(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.MultiFaceDefectPins, faceQualificationReports, qualifiedProfiles)
		if multiErr != v2qualification.ErrUnqualified || multiFaces != nil {
			t.Fatal("unqualified second collection accepted", multiFaces, multiErr)
		}
		if _, err := evidenceOwner.Exec(ctx, `UPDATE photo.task_attempts SET profile=jsonb_set(profile,'{runtime_lock_sha}',to_jsonb($2::text)) WHERE task_id=$1`, secondTask, strings.Repeat("b", 64)); err != nil {
			t.Fatal(err)
		}
		t.Log("V27_DEFECT_QUALIFIED_MULTI_COLLECTION_OK")

		for _, q := range fixture.FaceQualifications {
			if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=$1", q.PolicyID); err != nil {
				t.Fatal(err)
			}
			rejected, qualificationErr := reader.ReadQualifiedDefectFaces(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.FaceDefectPins, fixture.DefectLandmark, fixture.DefectDetection, faceQualificationReports, qualifiedProfiles)
			if qualificationErr != v2qualification.ErrUnqualified || rejected != nil {
				t.Fatal("disabled face admission policy accepted", qualificationErr)
			}
			if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.v2_qualification_policies SET admission_enabled=true WHERE id=$1", q.PolicyID); err != nil {
				t.Fatal(err)
			}
		}
		t.Log("V27_DEFECT_FACE_QUALIFICATION_POLICY_DISABLED_DENIED_OK")
		for i, q := range fixture.FaceQualifications {
			reportObject, err := reader.faceQualificationObject(ctx, who, fixture.Tenant, qualifiedProfiles[i], faceQualificationReports[i])
			if err != nil {
				t.Fatal(err)
			}
			for _, revokeReport := range []bool{false, true} {
				qualificationRace := &afterSecondMaskOpen{delegate: objects, key: reportObject.object.Key, opens: 1, after: func(ctx context.Context) error {
					if revokeReport {
						_, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 day' WHERE id=$1", q.Report.ArtifactID)
						return err
					}
					_, err := evidenceOwner.Exec(ctx, "UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=$1", q.PolicyID)
					return err
				}}
				qualificationRaceReader := reader
				qualificationRaceReader.Objects = qualificationRace
				rejected, qualificationErr := qualificationRaceReader.ReadQualifiedDefectFaces(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.FaceDefectPins, fixture.DefectLandmark, fixture.DefectDetection, faceQualificationReports, qualifiedProfiles)
				if qualificationErr != v2qualification.ErrUnqualified || rejected != nil || !qualificationRace.done {
					t.Fatal("face qualification revoked during read accepted", i, revokeReport, qualificationErr)
				}
				if revokeReport {
					if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=NULL WHERE id=$1", q.Report.ArtifactID); err != nil {
						t.Fatal(err)
					}
				} else {
					if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.v2_qualification_policies SET admission_enabled=true WHERE id=$1", q.PolicyID); err != nil {
						t.Fatal(err)
					}
				}
			}
			corruptReport := &corruptDefectOpen{delegate: objects, key: reportObject.object.Key}
			corruptReportReader := reader
			corruptReportReader.Objects = corruptReport
			rejected, qualificationErr := corruptReportReader.ReadQualifiedDefectFaces(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.FaceDefectPins, fixture.DefectLandmark, fixture.DefectDetection, faceQualificationReports, qualifiedProfiles)
			if qualificationErr == nil || rejected != nil || !corruptReport.done {
				t.Fatal("corrupt face qualification accepted", i, qualificationErr)
			}
		}
		t.Log("V27_DEFECT_FACE_QUALIFICATION_READ_REVOCATION_DENIED_OK")

		detectionObject, err := reader.defectDetectionObject(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.FaceDefectPins, fixture.DefectLandmark, fixture.DefectDetection)
		if err != nil {
			t.Fatal("canonical detection object", err)
		}
		if _, err := reader.defectDetectionObject(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.FaceDefectPins, fixture.DefectLandmark, fixture.PersonReport); err != v2input.ErrInvalid {
			t.Fatal("foreign detector output accepted", err)
		}
		faceParamObjects, err := reader.defectFaceParamsObjects(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.FaceDefectPins, fixture.DefectLandmark, fixture.DefectDetection)
		if err != nil {
			t.Fatal("original face Params catalog", err)
		}
		for _, entry := range faceParamObjects {
			paramsRace := &afterSecondMaskOpen{delegate: objects, key: entry.object.Key, opens: 1, after: func(ctx context.Context) error {
				_, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 day' WHERE id=$1", entry.ref.ArtifactID)
				return err
			}}
			paramsReader := reader
			paramsReader.Objects = paramsRace
			rejected, paramErr := paramsReader.ReadDefectFaces(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.FaceDefectPins)
			if paramErr != v2input.ErrInvalid || rejected != nil || !paramsRace.done {
				t.Fatal("face Params expired during read accepted", paramErr)
			}
			if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=NULL WHERE id=$1", entry.ref.ArtifactID); err != nil {
				t.Fatal(err)
			}
			corruptParams := &corruptDefectOpen{delegate: objects, key: entry.object.Key}
			paramsReader.Objects = corruptParams
			rejected, paramErr = paramsReader.ReadDefectFaces(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.FaceDefectPins)
			if paramErr == nil || rejected != nil || !corruptParams.done {
				t.Fatal("corrupt face Params accepted", paramErr)
			}
		}
		t.Log("V27_DEFECT_FACE_PARAMS_EXPIRY_CORRUPTION_DENIED_OK")
		detectionRace := &afterSecondMaskOpen{delegate: objects, key: detectionObject.Key, opens: 1, after: func(ctx context.Context) error {
			_, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 day' WHERE id=$1", fixture.DefectDetection.ArtifactID)
			return err
		}}
		detectionRaceReader := reader
		detectionRaceReader.Objects = detectionRace
		detectionRejected, detectionRaceErr := detectionRaceReader.ReadDefectFaces(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.FaceDefectPins)
		if detectionRaceErr != v2input.ErrInvalid || detectionRejected != nil || !detectionRace.done {
			t.Fatal("detection expiry during read accepted", detectionRejected, detectionRaceErr)
		}
		if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=NULL WHERE id=$1", fixture.DefectDetection.ArtifactID); err != nil {
			t.Fatal(err)
		}
		detectionCorrupt := &corruptDefectOpen{delegate: objects, key: detectionObject.Key}
		detectionCorruptReader := reader
		detectionCorruptReader.Objects = detectionCorrupt
		detectionRejected, detectionCorruptErr := detectionCorruptReader.ReadDefectFaces(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.FaceDefectPins)
		if detectionCorruptErr == nil || detectionRejected != nil || !detectionCorrupt.done {
			t.Fatal("corrupt detection bytes accepted", detectionRejected, detectionCorruptErr)
		}
		t.Log("V27_DEFECT_DETECTION_ORIGIN_AND_READ_OK")
		t.Log("V27_DEFECT_DETECTION_EXPIRY_CORRUPTION_DENIED_OK")
		landmarkObject, err := reader.defectLandmarkObject(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.FaceDefectPins, fixture.DefectLandmark)
		if err != nil {
			t.Fatal(err)
		}
		corruptLandmark := &corruptDefectOpen{delegate: objects, key: landmarkObject.Key}
		corruptLandmarkReader := reader
		corruptLandmarkReader.Objects = corruptLandmark
		corruptFaces, corruptFaceErr := corruptLandmarkReader.ReadDefectFaces(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.FaceDefectPins)
		if corruptFaceErr == nil || corruptFaces != nil || !corruptLandmark.done {
			t.Fatal("corrupt landmark bytes accepted", corruptFaces, corruptFaceErr)
		}
		t.Log("V27_DEFECT_LANDMARK_CORRUPTION_DENIED_OK")
		landmarkRace := &afterSecondMaskOpen{delegate: objects, key: landmarkObject.Key, opens: 1, after: func(ctx context.Context) error {
			_, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 day' WHERE id=$1", fixture.DefectLandmark.ArtifactID)
			return err
		}}
		landmarkRaceReader := reader
		landmarkRaceReader.Objects = landmarkRace
		rejectedFaces, faceRaceErr := landmarkRaceReader.ReadDefectFaces(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.FaceDefectPins)
		if faceRaceErr != v2input.ErrInvalid || rejectedFaces != nil || !landmarkRace.done {
			t.Fatal("landmark expired during read accepted", rejectedFaces, faceRaceErr)
		}
		if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=NULL WHERE id=$1", fixture.DefectLandmark.ArtifactID); err != nil {
			t.Fatal(err)
		}
		t.Log("V27_DEFECT_NONEMPTY_FACES_READ_OK")
		t.Log("V27_DEFECT_LANDMARK_UNDECLARED_DENIED_OK")
		t.Log("V27_DEFECT_LANDMARK_READ_RACE_DENIED_OK")

		permanentCount, permanentErr := reader.VerifyDefectPermanentCoverage(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.DefectPins, fixture.RequiredProtection)
		if permanentErr != nil || permanentCount != 1 {
			t.Fatal("defect permanent coverage", permanentCount, permanentErr)
		}
		permanentCount, permanentErr = reader.VerifyDefectPermanentCoverage(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.DefectPins, fixture.Protection)
		if permanentErr != v2input.ErrInvalid || permanentCount != 0 {
			t.Fatal("undeclared permanent mask accepted", permanentCount, permanentErr)
		}
		t.Log("V27_DEFECT_PERMANENT_COVERAGE_OK")

		defectObjects, err := reader.defectEvidenceObjects(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.DefectPins)
		if err != nil {
			t.Fatal(err)
		}
		for _, objectIndex := range []int{3, 4} {
			corruptReader := reader
			corruptObjects := &corruptDefectOpen{delegate: objects, key: defectObjects[objectIndex].object.Key}
			corruptReader.Objects = corruptObjects
			rejected, err := corruptReader.ReadDefectEvidence(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.DefectPins)
			if err == nil || !corruptObjects.done || len(rejected.Params) != 0 || len(rejected.Proposal) != 0 || len(rejected.Report) != 0 || rejected.Labels.ArtifactID != uuid.Nil || rejected.Protection.ArtifactID != uuid.Nil {
				t.Fatal("corrupt defect pixels accepted", objectIndex, err)
			}
		}
		t.Log("V27_DEFECT_CORRUPT_PIXELS_DENIED_OK")
		versionDenied := false
		versionRace := &afterSecondMaskOpen{delegate: objects, key: defectObjects[3].object.Key, opens: 1, after: func(ctx context.Context) error {
			_, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET object_version='replacement-version' WHERE id=$1", fixture.DefectPins.Labels.ArtifactID)
			var postgres *pgconn.PgError
			versionDenied = errors.As(err, &postgres) && postgres.Code == "23514"
			if !versionDenied {
				return fmt.Errorf("expected immutable object version denial: %w", err)
			}
			return nil
		}}
		versionRaceReader := reader
		versionRaceReader.Objects = versionRace
		versionEvidence, versionErr := versionRaceReader.ReadDefectEvidence(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.DefectPins)
		if versionErr != nil || !versionRace.done || !versionDenied || versionEvidence.Labels != fixture.DefectPins.Labels || versionEvidence.Protection != fixture.DefectPins.Protection {
			t.Fatal("immutable defect version protection", versionErr, versionDenied)
		}
		t.Log("V27_DEFECT_OBJECT_VERSION_IMMUTABLE_OK")

		permanentRace := &afterSecondMaskOpen{delegate: objects, key: defectObjects[4].object.Key, after: func(ctx context.Context) error {
			_, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 day' WHERE id=$1", fixture.DefectPins.Protection.ArtifactID)
			return err
		}}
		permanentRaceReader := reader
		permanentRaceReader.Objects = permanentRace
		permanentCount, permanentErr = permanentRaceReader.VerifyDefectPermanentCoverage(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.DefectPins, fixture.RequiredProtection)
		if permanentErr != v2input.ErrInvalid || permanentCount != 0 || !permanentRace.done {
			t.Fatal("defect protection expired during coverage accepted", permanentCount, permanentErr)
		}
		if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=NULL WHERE id=$1", fixture.DefectPins.Protection.ArtifactID); err != nil {
			t.Fatal(err)
		}
		t.Log("V27_DEFECT_PERMANENT_COVERAGE_RACE_DENIED_OK")
		// Seed opens=1 to fire this existing wrapper on the first report read.
		defectRace := &afterSecondMaskOpen{delegate: objects, key: defectObjects[2].object.Key, opens: 1, after: func(ctx context.Context) error {
			_, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 day' WHERE id=$1", fixture.DefectPins.Report.ArtifactID)
			return err
		}}
		defectRaceReader := reader
		defectRaceReader.Objects = defectRace
		rejectedDefect, defectRaceErr := defectRaceReader.ReadDefectEvidence(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.DefectPins)
		if defectRaceErr != v2input.ErrInvalid || !defectRace.done || len(rejectedDefect.Params) != 0 || len(rejectedDefect.Report) != 0 || len(rejectedDefect.Proposal) != 0 || rejectedDefect.Labels.ArtifactID != uuid.Nil || rejectedDefect.Protection.ArtifactID != uuid.Nil {
			t.Fatal("defect report expired during read accepted", defectRaceErr, defectRace.done)
		}
		if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=NULL WHERE id=$1", fixture.DefectPins.Report.ArtifactID); err != nil {
			t.Fatal(err)
		}
		t.Log("V27_DEFECT_REPORT_READ_RACE_DENIED_OK")

		t.Log("V27_PROPOSAL_PROTECTION_READ_OK")
		coverage, coverageErr := reader.VerifyProtectionCoverage(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.Protection, fixture.Protection)
		if coverageErr != nil || coverage != 0 {
			t.Fatal("verified empty coverage", coverage, coverageErr)
		}
		coverage, coverageErr = reader.VerifyProtectionCoverage(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.RequiredProtection, fixture.RequiredProtection)
		if coverageErr != nil || coverage != 1 {
			t.Fatal("nonempty coverage", coverage, coverageErr)
		}
		coverage, coverageErr = reader.VerifyProtectionCoverage(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.RequiredProtection, fixture.Protection)
		if !errors.Is(coverageErr, masknpy.ErrInvalid) || coverage != 0 {
			t.Fatal("uncovered required pixel accepted", coverage, coverageErr)
		}
		t.Log("V27_PROPOSAL_COVERAGE_REQUIRED_OK")
		count, err := reader.VerifySelectionSeparation(ctx, who, request, fixture.Protection)
		if err != nil || count != 119999 {
			t.Fatal("selection/protection separation", count, err)
		}
		t.Log("V27_PROPOSAL_MASK_SEPARATION_OK")
		q := fixture.Qualification
		trusted := v2qualification.Expected{Operation: "semantic.transfer", Version: "1", Backend: "cuda", ModelLockSHA: q.ModelLockSHA, RuntimeLockSHA: q.RuntimeLockSHA, DeviceProfileSHA: q.DeviceProfileSHA, DatasetSHA: q.DatasetSHA, IssuerID: q.IssuerID, RequiredChecks: []string{"quality"}, TestFixtureSHAs: map[string]struct{}{strings.Repeat("e", 64): {}}, EvidenceSHAs: map[string]struct{}{strings.Repeat("f", 64): {}}, CalibrationSHAs: map[string]struct{}{q.Calibration.SHA256: {}}, LicenseCleared: true}
		if err := reader.VerifyQualification(ctx, who, fixture.Tenant, q, trusted); err != nil {
			t.Fatal("qualification object bytes", err)
		}
		personExpected := trusted
		personExpected.Operation = "mask.person"
		rejectedPerson, personQualificationErr := reader.ReadQualifiedPersonEvidence(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.PersonMask, q.Report, personExpected)
		if personQualificationErr != v2qualification.ErrUnqualified || rejectedPerson.Root.ArtifactID != uuid.Nil {
			t.Fatal("transfer qualification accepted for person", personQualificationErr)
		}
		t.Log("V27_PROPOSAL_PERSON_QUALIFICATION_SEPARATE_OK")
		qualifiedPerson, qualifiedPersonErr := reader.ReadQualifiedPersonEvidence(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.PersonMask, fixture.PersonQualification.Report, personExpected)
		if qualifiedPersonErr != nil || qualifiedPerson.Root != fixture.PersonMask {
			t.Fatal("qualified person evidence", qualifiedPersonErr)
		}
		discovered, discoveryErr := reader.SelectQualifiedPersonEvidence(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.PersonQualification.Report, personExpected)
		if discoveryErr != nil || discovered.Root != fixture.PersonMask || discovered.Pixels != qualifiedPerson.Pixels {
			t.Fatal("automatic person discovery", discoveryErr)
		}
		selectedProtectionPerson, selectedProtectionCount, selectedProtectionErr := reader.SelectQualifiedPersonProtectionCoverage(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.RequiredProtection, fixture.PersonQualification.Report, personExpected)
		if selectedProtectionErr != nil || selectedProtectionCount != 1 || selectedProtectionPerson != fixture.PersonMask {
			t.Fatal("automatic qualified person protection", selectedProtectionCount, selectedProtectionErr)
		}
		uncoveredPerson, uncoveredCount, uncoveredErr := reader.SelectQualifiedPersonProtectionCoverage(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.Protection, fixture.PersonQualification.Report, personExpected)
		if !errors.Is(uncoveredErr, masknpy.ErrInvalid) || uncoveredCount != 0 || uncoveredPerson.ArtifactID != uuid.Nil {
			t.Fatal("automatic person uncovered protection accepted", uncoveredCount, uncoveredErr)
		}
		missingBinding := request.Intent.Reference
		missingBinding.AssetVersionID = uuid.New()
		missingPerson, missingPersonErr := reader.SelectQualifiedPersonEvidence(ctx, who, fixture.Tenant, missingBinding, fixture.PersonQualification.Report, personExpected)
		if missingPersonErr != v2input.ErrInvalid || missingPerson.Root.ArtifactID != uuid.Nil {
			t.Fatal("missing version person fallback", missingPersonErr)
		}
		t.Log("V27_PROPOSAL_PERSON_DISCOVERY_OK")
		if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=NULL WHERE id=$1", fixture.AmbiguousPersonMask.ArtifactID); err != nil {
			t.Fatal(err)
		}
		ambiguousPerson, ambiguousErr := reader.SelectQualifiedPersonEvidence(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.PersonQualification.Report, personExpected)
		if !errors.Is(ambiguousErr, v2input.ErrConflict) || ambiguousPerson.Root.ArtifactID != uuid.Nil || ambiguousPerson.Pixels.ArtifactID != uuid.Nil {
			t.Fatal("ambiguous person selection accepted", ambiguousErr)
		}
		if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 day' WHERE id=$1", fixture.AmbiguousPersonMask.ArtifactID); err != nil {
			t.Fatal(err)
		}
		restoredPerson, restoredErr := reader.SelectQualifiedPersonEvidence(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.PersonQualification.Report, personExpected)
		if restoredErr != nil || restoredPerson.Root != fixture.PersonMask {
			t.Fatal("unique person selection after expiration", restoredErr)
		}
		t.Log("V27_PROPOSAL_PERSON_AMBIGUITY_OK")
		candidateRaceObjects := &afterSecondMaskOpen{delegate: objects, key: coveringObject.Key, after: func(ctx context.Context) error {
			_, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=NULL WHERE id=$1", fixture.AmbiguousPersonMask.ArtifactID)
			return err
		}}
		candidateRaceReader := reader
		candidateRaceReader.Objects = candidateRaceObjects
		racePerson, racePersonCount, racePersonErr := candidateRaceReader.SelectQualifiedPersonProtectionCoverage(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.RequiredProtection, fixture.PersonQualification.Report, personExpected)
		if !errors.Is(racePersonErr, v2input.ErrConflict) || racePersonCount != 0 || racePerson.ArtifactID != uuid.Nil || !candidateRaceObjects.done {
			t.Fatal("person ambiguity during coverage accepted", racePersonCount, racePersonErr, candidateRaceObjects.done)
		}
		if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 day' WHERE id=$1", fixture.AmbiguousPersonMask.ArtifactID); err != nil {
			t.Fatal(err)
		}
		t.Log("V27_PROPOSAL_PERSON_SELECTION_COVERAGE_RACE_OK")

		if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.task_attempts a SET profile=jsonb_set(a.profile,'{runtime_lock_sha}',to_jsonb($2::text)) FROM photo.artifacts root WHERE root.tenant_id=a.tenant_id AND root.created_by_task_id=a.task_id AND root.id=$1", fixture.PersonMask.ArtifactID, strings.Repeat("0", 64)); err != nil {
			t.Fatal(err)
		}
		wrongRuntime, wrongRuntimeErr := reader.ReadQualifiedPersonEvidence(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.PersonMask, fixture.PersonQualification.Report, personExpected)
		if wrongRuntimeErr != v2qualification.ErrUnqualified || wrongRuntime.Root.ArtifactID != uuid.Nil {
			t.Fatal("person attempt runtime substitution accepted", wrongRuntimeErr)
		}
		if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.task_attempts a SET profile=jsonb_set(a.profile,'{runtime_lock_sha}',to_jsonb($2::text)) FROM photo.artifacts root WHERE root.tenant_id=a.tenant_id AND root.created_by_task_id=a.task_id AND root.id=$1", fixture.PersonMask.ArtifactID, personExpected.RuntimeLockSHA); err != nil {
			t.Fatal(err)
		}
		t.Log("V27_PROPOSAL_PERSON_PROFILE_LOCK_OK")
		if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=$1", fixture.PersonQualification.PolicyID); err != nil {
			t.Fatal(err)
		}
		disabledPerson, disabledErr := reader.ReadQualifiedPersonEvidence(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.PersonMask, fixture.PersonQualification.Report, personExpected)
		if disabledErr != v2qualification.ErrUnqualified || disabledPerson.Root.ArtifactID != uuid.Nil {
			t.Fatal("disabled person policy accepted", disabledErr)
		}
		if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.v2_qualification_policies SET admission_enabled=true WHERE id=$1", fixture.PersonQualification.PolicyID); err != nil {
			t.Fatal(err)
		}
		qualifiedCount, qualifiedCoverageErr := reader.VerifyQualifiedPersonProtectionCoverage(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.PersonMask, fixture.RequiredProtection, fixture.PersonQualification.Report, personExpected)
		if qualifiedCoverageErr != nil || qualifiedCount != 1 {
			t.Fatal("qualified person coverage", qualifiedCount, qualifiedCoverageErr)
		}
		qualificationRaceObjects := &afterSecondMaskOpen{delegate: objects, key: coveringObject.Key, after: func(ctx context.Context) error {
			_, err := evidenceOwner.Exec(ctx, "UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=$1", fixture.PersonQualification.PolicyID)
			return err
		}}
		qualificationRaceReader := reader
		qualificationRaceReader.Objects = qualificationRaceObjects
		revokedCount, revokedCoverageErr := qualificationRaceReader.VerifyQualifiedPersonProtectionCoverage(ctx, who, fixture.Tenant, request.Intent.Reference, fixture.PersonMask, fixture.RequiredProtection, fixture.PersonQualification.Report, personExpected)
		if revokedCoverageErr != v2qualification.ErrUnqualified || revokedCount != 0 || !qualificationRaceObjects.done {
			t.Fatal("person policy revoked during coverage accepted", revokedCount, revokedCoverageErr, qualificationRaceObjects.done)
		}
		if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.v2_qualification_policies SET admission_enabled=true WHERE id=$1", fixture.PersonQualification.PolicyID); err != nil {
			t.Fatal(err)
		}
		t.Log("V27_PROPOSAL_PERSON_QUALIFICATION_RACE_OK")
		t.Log("V27_PROPOSAL_PERSON_QUALIFIED_COVERAGE_OK")
		t.Log("V27_PROPOSAL_PERSON_QUALIFICATION_OK")

		wrongPolicy := trusted
		wrongPolicy.RuntimeLockSHA = strings.Repeat("0", 64)
		if err := reader.VerifyQualification(ctx, who, fixture.Tenant, q, wrongPolicy); err == nil {
			t.Fatal("foreign runtime qualification")
		}
		t.Log("V27_PROPOSAL_QUALIFICATION_BYTES_OK")
		preparation = TransferPreparation{Qualification: q, Expected: trusted, Color: color, RAM: 1 << 30, MetadataBytes: 64 << 20, VRAM: 1 << 30, Scratch: 1 << 30, Output: 1 << 30, SourceProtection: fixture.Protection, TargetProtection: []v2input.Ref{fixture.Protection}}
		if fixture.MeasuredTransferCosts {
			preparation.Costs = v2input.SemanticQualifiedCosts{RAMBytes: 101, VRAMBytes: 202, ScratchBytes: 303}
			understated := preparation
			understated.Costs.VRAMBytes--
			rejectedInputs, rejectedDocuments, rejectedErr := reader.PrepareTransferInputs(ctx, who, request, first, understated)
			if rejectedErr != v2input.ErrInvalid || rejectedInputs != nil || rejectedDocuments != nil {
				t.Fatal("original measured VRAM underestimation accepted", rejectedErr)
			}
			t.Log("V27_TRANSFER_MEASURED_COST_DENIAL_OK")
		}
		preparedInputs, preparedDocuments, err = reader.PrepareTransferInputs(ctx, who, request, first, preparation)
		if err != nil || len(preparedInputs) != 1 || len(preparedDocuments) != 1 {
			t.Fatal("full transfer inputs", err)
		}
		personPreparation := preparation
		personPreparation.PersonProtection = &TransferPersonProtection{Report: fixture.PersonQualification.Report, Expected: personExpected}
		if fixture.CommitOrdinary || fixture.CreateOrdinary {
			deployment := ownTransferPreparation(personPreparation)
			deployment.SourceProtection = v2input.Ref{}
			deployment.TargetProtection = nil
			if fixture.CreatePerson {
				deployment.RAM = 2 << 30
			}
			serviceCfg := cfg.Copy()
			serviceCfg.ConnConfig.RuntimeParams["role"] = "photo_scheduler"
			serviceScheduler, err := pgxpool.NewWithConfig(ctx, serviceCfg)
			if err != nil {
				t.Fatal(err)
			}
			defer serviceScheduler.Close()
			serviceTransport := artifactjson.Publisher{Repository: artifactjson.SQLRepository{DB: serviceScheduler, Bucket: fixture.Objects.Bucket}, Objects: objects}
			service := CreateService{Preparer: TransferDraftPreparer{Reader: reader, Documents: publisher, Scheduler: serviceScheduler, TransportDocuments: serviceTransport, Configuration: deployment, ReservedBytes: 1 << 28}}
			swapped := service
			swapped.Preparer.Reader.DB = serviceScheduler
			swapped.Preparer.Documents.DB = serviceScheduler
			swapped.Preparer.Scheduler = api
			if response, err := swapped.Propose(ctx, who, request.TenantID, "proposal-publication", request.Intent); !errors.Is(err, v2input.ErrInvalid) || response.Status != 0 || response.ResourceID != nil || len(response.Body) != 0 {
				t.Fatal("swapped service roles reached publication", err)
			}
			t.Log("V27_CREATE_SWAPPED_ROLES_REFUSED_OK")
			response, createErr := service.Propose(ctx, who, request.TenantID, "proposal-publication", request.Intent)
			if !errors.Is(createErr, v2input.ErrConflict) || response.Status != 0 || response.ResourceID != nil || len(response.Body) != 0 {
				t.Fatal("ambiguous protection produced public receipt", createErr)
			}
			t.Log("V27_CREATE_AMBIGUOUS_POLICY_REFUSED_OK")
			// Disposable fixture only: make the independently covered producer the
			// sole live candidate, retaining exact IDs for restoration afterwards.
			rows, err := evidenceOwner.Query(ctx, `UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second'
				WHERE tenant_id=$1 AND id<>$2 AND expires_at IS NULL AND created_by_task_id IS NOT NULL
				AND output_role IN ('mask_protected_ref','defect_protection_ref','transformed_mask_ref','mask')
				AND media_type='application/json' RETURNING id`, fixture.Tenant, fixture.RequiredProtection.ArtifactID)
			if err != nil {
				t.Fatal(err)
			}
			var retired []uuid.UUID
			for rows.Next() {
				var id uuid.UUID
				if err := rows.Scan(&id); err != nil {
					t.Fatal(err)
				}
				retired = append(retired, id)
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			selected, selectionErr := reader.selectOrdinaryTransferConfiguration(ctx, who, request, first, deployment)
			if fixture.CreateOrdinary && selectionErr == nil {
				if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.operation_registry SET v2_enabled=true WHERE operation='semantic.transfer' AND version='1'"); err != nil {
					t.Fatal(err)
				}
				if fixture.CreatePerson {
					if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.operation_registry SET v2_enabled=true WHERE operation='mask.person' AND version='1'"); err != nil {
						t.Fatal(err)
					}
				}
				probeCommittedCreateService(t, ctx, service, who, request, evidenceOwner, fixture.CreatePerson)
				if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.operation_registry SET v2_enabled=false WHERE operation='semantic.transfer' AND version='1'"); err != nil {
					t.Fatal(err)
				}
				if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=NULL WHERE tenant_id=$1 AND id=ANY($2::uuid[])", fixture.Tenant, retired); err != nil {
					t.Fatal(err)
				}
				return
			}
			if _, err := evidenceOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=NULL WHERE tenant_id=$1 AND id=ANY($2::uuid[])", fixture.Tenant, retired); err != nil {
				t.Fatal(err)
			}
			if selectionErr != nil || selected.SourceProtection != fixture.RequiredProtection || len(selected.TargetProtection) != 1 || selected.TargetProtection[0] != fixture.RequiredProtection {
				t.Fatal("unique independently covered protection selection", selectionErr)
			}
			t.Log("V27_CREATE_UNIQUE_POLICY_SELECTED_OK")
			selectedInputs, selectedDocuments, selectedErr := reader.PrepareTransferInputs(ctx, who, request, first, selected)
			if selectedErr != nil || len(selectedInputs) != 1 || len(selectedDocuments) != 1 {
				t.Fatal("selected protection could not prepare transfer inputs", selectedErr)
			}
			if selectedInputs[0].Dependencies.SourceProtection != fixture.RequiredProtection || selectedInputs[0].Dependencies.TargetProtection != fixture.RequiredProtection || selectedInputs[0].sourcePerson == nil || selectedInputs[0].targetPerson == nil || selectedInputs[0].sourcePerson.Count != 1 || selectedInputs[0].targetPerson.Count != 1 {
				t.Fatal("automatic selection lost original protection or person coverage")
			}
			t.Log("V27_CREATE_SELECTED_INPUTS_PREPARED_OK")
		}
		if personInputs, personDocuments, personErr := reader.PrepareTransferInputs(ctx, who, request, first, personPreparation); !errors.Is(personErr, masknpy.ErrInvalid) || personInputs != nil || personDocuments != nil {
			t.Fatal("transfer preparation ignored person coverage", personErr)
		}
		t.Log("V27_TRANSFER_PERSON_PROTECTION_REQUIRED_OK")
		personPreparation.SourceProtection = fixture.RequiredProtection
		personPreparation.TargetProtection = []v2input.Ref{fixture.RequiredProtection}
		personInputs, personDocuments, personErr := reader.PrepareTransferInputs(ctx, who, request, first, personPreparation)
		if personErr != nil || len(personInputs) != 1 || len(personDocuments) != 1 || personInputs[0].Dependencies.SourceProtection != fixture.RequiredProtection || personInputs[0].Dependencies.TargetProtection != fixture.RequiredProtection {
			t.Fatal("covered transfer person preparation", personErr)
		}
		personPreparedInputs, personPreparedDocuments = personInputs, personDocuments
		if personInputs[0].sourcePerson == nil || personInputs[0].targetPerson == nil || personInputs[0].sourcePerson.Count != 1 || personInputs[0].targetPerson.Count != 1 {
			t.Fatal("missing verified person preparation proof")
		}
		// Calculate the retained batch size independently from the production charger.
		// The original protected-mask object pins must count alongside person JSON.
		retainedPersonBytes := int64(1024)
		budgetQualification, err := reader.readTransferQualificationPreparation(ctx, who, fixture.Tenant, q, trusted)
		if err != nil {
			t.Fatal(err)
		}
		retainedPersonBytes += int64(1024 + len(budgetQualification.expected))
		for _, sourceProof := range []*sourcePreparationProof{personInputs[0].referenceSource, personInputs[0].targetSource} {
			if sourceProof == nil {
				t.Fatal("missing retained source proof")
			}
			retainedPersonBytes += int64(1024 + len(sourceProof.pins))
		}
		for _, object := range budgetQualification.objects {
			retainedPersonBytes += int64(len(budgetQualification.bucket) + len(object.object.Key) + len(object.object.VersionID))
		}
		for i, document := range personDocuments {
			retainedPersonBytes += int64(len(document.Reference) + len(document.Target) + len(document.SelectedMask) + len(document.SourceProtection) + len(document.TargetProtection) + len(personInputs[i].Profile))
			for _, proof := range []*personProtectionProof{personInputs[i].sourcePerson, personInputs[i].targetPerson} {
				pins := proof.protectionEvidence
				retainedPersonBytes += int64(len(proof.Expected) + len(proof.Evidence) + len(proof.Qualification) + 1024 + len(pins.bucket) + len(pins.root.object.Key) + len(pins.root.object.VersionID) + len(pins.pixels.object.Key) + len(pins.pixels.object.VersionID))
			}
		}
		boundaryPreparation := ownTransferPreparation(personPreparation)
		boundaryPreparation.MetadataBytes = retainedPersonBytes - 1
		if rejectedInputs, rejectedDocuments, rejectedErr := reader.PrepareTransferInputs(ctx, who, request, first, boundaryPreparation); rejectedErr != v2input.ErrInvalid || rejectedInputs != nil || rejectedDocuments != nil {
			t.Fatal("person retained metadata overflow returned partial batch", rejectedErr)
		}
		boundaryPreparation.MetadataBytes = retainedPersonBytes
		if acceptedInputs, acceptedDocuments, acceptedErr := reader.PrepareTransferInputs(ctx, who, request, first, boundaryPreparation); acceptedErr != nil || len(acceptedInputs) != len(personInputs) || len(acceptedDocuments) != len(personDocuments) {
			t.Fatal("person retained metadata exact boundary rejected", acceptedErr)
		}
		t.Log("V27_TRANSFER_PERSON_METADATA_BOUNDARY_OK")
		mutablePreparation := ownTransferPreparation(personPreparation)
		policyMutationObjects := &afterSecondMaskOpen{delegate: objects, key: coveringObject.Key, after: func(ctx context.Context) error {
			mutablePreparation.PersonProtection.Report = q.Report
			mutablePreparation.PersonProtection.Expected.RuntimeLockSHA = strings.Repeat("0", 64)
			mutablePreparation.PersonProtection.Expected.RequiredChecks[0] = "unapproved"
			clear(mutablePreparation.PersonProtection.Expected.EvidenceSHAs)
			clear(mutablePreparation.Expected.TestFixtureSHAs)
			mutablePreparation.TargetProtection[0] = fixture.Protection
			return nil
		}}
		policyMutationReader := reader
		policyMutationReader.Objects = policyMutationObjects
		ownedInputs, ownedDocuments, ownedErr := policyMutationReader.PrepareTransferInputs(ctx, who, request, first, mutablePreparation)
		if ownedErr != nil || len(ownedInputs) != 1 || len(ownedDocuments) != 1 || ownedInputs[0].Dependencies.TargetProtection != fixture.RequiredProtection || !policyMutationObjects.done {
			t.Fatal("transfer configuration changed during IO", ownedErr, policyMutationObjects.done)
		}
		t.Log("V27_TRANSFER_POLICY_OWNERSHIP_OK")
		personPreparation.TargetProtection = []v2input.Ref{fixture.Protection}
		if targetInputs, targetDocuments, targetErr := reader.PrepareTransferInputs(ctx, who, request, first, personPreparation); !errors.Is(targetErr, masknpy.ErrInvalid) || targetInputs != nil || targetDocuments != nil {
			t.Fatal("transfer target ignored person coverage", targetErr)
		}
		t.Log("V27_TRANSFER_PERSON_TARGET_PROTECTION_OK")

		limited := preparation
		limited.MetadataBytes = 1024
		if partialInputs, partialDocuments, err := reader.PrepareTransferInputs(ctx, who, request, first, limited); err != v2input.ErrInvalid || partialInputs != nil || partialDocuments != nil {
			t.Fatal("metadata budget returned partial batch", err)
		}
		t.Log("V27_PROPOSAL_METADATA_BUDGET_OK")
		t.Log("V27_PROPOSAL_PREPARED_INPUTS_OK")
		refs, err := PublishTransferParams(ctx, publisher, who, request, "proposal-publication", first, strings.Repeat("c", 64), fixture.Qualification.Calibration)
		if err != nil || len(refs) != 1 {
			t.Fatal("batch transfer publication", err)
		}
		replay, err := PublishTransferParams(ctx, publisher, who, request, "proposal-publication", first, strings.Repeat("c", 64), fixture.Qualification.Calibration)
		if err != nil || len(replay) != 1 || replay[0] != refs[0] {
			t.Fatal("batch transfer replay", err)
		}
		if _, err := PublishTransferParams(ctx, publisher, who, request, "proposal-publication", first, strings.Repeat("d", 64), fixture.Qualification.Calibration); err == nil {
			t.Fatal("changed batch accepted")
		}
		transferRefs = refs
		t.Log("V27_TRANSFER_BATCH_PUBLICATION_OK")
	}
	schedulerCfg := cfg.Copy()
	schedulerCfg.ConnConfig.RuntimeParams["role"] = "photo_scheduler"
	scheduler, err := pgxpool.NewWithConfig(ctx, schedulerCfg)
	if err != nil {
		t.Fatal("scheduler connection")
	}
	defer scheduler.Close()
	tx, err := scheduler.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err := LockInventoryInTransaction(ctx, tx, who, request, first.Inventory); err != nil {
		t.Fatal("inventory transaction adapter", err)
	}
	if err := LockPublishedIntentInTransaction(ctx, tx, who, request, first); err != nil {
		t.Fatal("published intent lock", err)
	}

	var plan WorkflowPlan
	if fixture.TransferBatch {
		plan, err = CompileTransferWorkflowPlanFromDocuments(request, who.SessionID, first, snapshotRef, snapshotBody, transferRefs, preparedInputs, fixture.Qualification, preparedDocuments)
		if err != nil {
			t.Fatal("physical draft plan", err)
		}
		if plan.transferQualification == nil || preparedInputs[0].qualification == nil || *plan.transferQualification != *preparedInputs[0].qualification {
			t.Fatal("plan lost original transfer qualification proof")
		}
		if !plan.transferQualificationRequired {
			t.Fatal("prepared plan does not require original proof")
		}
		missingQualificationPlan := plan
		missingQualificationPlan.transferQualification = nil
		if err := LockQualificationInTransaction(ctx, tx, who, request, missingQualificationPlan); err != v2input.ErrInvalid {
			t.Fatal("prepared plan lost proof without rejection", err)
		}
		t.Log("V27_TRANSFER_PLAN_REQUIRED_QUALIFICATION_OK")
		retainedPlanProof := *plan.transferQualification
		preparedInputs[0].qualification.actor = uuid.New()
		if *plan.transferQualification != retainedPlanProof {
			t.Fatal("plan aliases preparer qualification proof")
		}
		preparedInputs[0].qualification.actor = retainedPlanProof.actor
		t.Log("V27_TRANSFER_PLAN_QUALIFICATION_OWNERSHIP_OK")
		personPlan, personPlanErr := CompileTransferWorkflowPlanFromDocuments(request, who.SessionID, first, snapshotRef, snapshotBody, transferRefs, personPreparedInputs, fixture.Qualification, personPreparedDocuments)
		if personPlanErr != nil || len(personPlan.person) != 2 || personPlan.person[0].Root != fixture.PersonMask || personPlan.person[0].Report != fixture.PersonQualification.Report || personPlan.person[0].Child.ArtifactID == uuid.Nil {
			t.Fatal("person proof dropped from plan", personPlanErr)
		}
		invalidPersonInputs := append([]TransferTaskInputs(nil), personPreparedInputs...)
		invalidPersonProof := *invalidPersonInputs[0].sourcePerson
		invalidPersonProof.Protection = fixture.Protection
		invalidPersonInputs[0].sourcePerson = &invalidPersonProof
		invalidPersonPlan, invalidPersonErr := CompileTransferWorkflowPlanFromDocuments(request, who.SessionID, first, snapshotRef, snapshotBody, transferRefs, invalidPersonInputs, fixture.Qualification, personPreparedDocuments)
		if invalidPersonErr != v2input.ErrInvalid || len(invalidPersonPlan.person) != 0 {
			t.Fatal("mismatched person protection proof compiled", invalidPersonErr)
		}
		invalidPersonInputs[0] = personPreparedInputs[0]
		invalidPersonInputs[0].targetPerson = nil
		if _, err := CompileTransferWorkflowPlanFromDocuments(request, who.SessionID, first, snapshotRef, snapshotBody, transferRefs, invalidPersonInputs, fixture.Qualification, personPreparedDocuments); err != v2input.ErrInvalid {
			t.Fatal("partial person proof compiled", err)
		}
		revocationOwner, err := pgxpool.New(ctx, os.Getenv("PHOTO_TEST_DATABASE_URL"))
		if err != nil {
			t.Fatal("person policy revocation connection")
		}
		defer revocationOwner.Close()
		if _, err := revocationOwner.Exec(ctx, "UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=$1", fixture.PersonQualification.PolicyID); err != nil {
			t.Fatal(err)
		}
		if err := LockPersonProducerInTransaction(ctx, tx, who, request, personPlan); err != v2input.ErrConflict {
			t.Fatal("revoked person admission policy locked", err)
		}
		if _, err := revocationOwner.Exec(ctx, "UPDATE photo.v2_qualification_policies SET admission_enabled=true WHERE id=$1", fixture.PersonQualification.PolicyID); err != nil {
			t.Fatal(err)
		}
		t.Log("V27_TRANSFER_PERSON_QUALIFICATION_REVOCATION_OK")
		if _, err := tx.Exec(ctx, "SAVEPOINT person_inventory_ambiguity"); err != nil {
			t.Fatal(err)
		}
		if _, err := revocationOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=NULL WHERE id=$1", fixture.AmbiguousPersonMask.ArtifactID); err != nil {
			t.Fatal(err)
		}
		if err := LockPersonProducerInTransaction(ctx, tx, who, request, personPlan); err != v2input.ErrConflict {
			t.Fatal("post-preflight ambiguous candidate admitted", err)
		}
		if _, err := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT person_inventory_ambiguity"); err != nil {
			t.Fatal(err)
		}
		if _, err := revocationOwner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 day' WHERE id=$1", fixture.AmbiguousPersonMask.ArtifactID); err != nil {
			t.Fatal(err)
		}
		t.Log("V27_TRANSFER_PERSON_ADMISSION_DISCOVERY_OK")

		if err := LockPersonProducerInTransaction(ctx, tx, who, request, personPlan); err != nil {
			t.Fatal("person producer lock", err)
		}
		rejectedPersonPlan := personPlan
		rejectedPersonPlan.person = append([]personProtectionProof(nil), personPlan.person...)
		rejectedPersonPlan.person[0].Child.SHA256 = strings.Repeat("0", 64)
		if err := LockPersonProducerInTransaction(ctx, tx, who, request, rejectedPersonPlan); err != v2input.ErrConflict {
			t.Fatal("foreign person pixels locked", err)
		}
		personOwner, err := pgxpool.New(ctx, os.Getenv("PHOTO_TEST_DATABASE_URL"))
		if err != nil {
			t.Fatal("person lock owner connection")
		}
		defer personOwner.Close()
		personProbe, err := personOwner.Acquire(ctx)
		if err != nil {
			t.Fatal("person lock probe", err)
		}
		defer personProbe.Release()
		if _, err := personProbe.Exec(ctx, "SET statement_timeout='250ms'"); err != nil {
			t.Fatal(err)
		}
		for _, query := range []string{
			"SELECT id FROM photo.tasks WHERE tenant_id=$1 AND id=(SELECT created_by_task_id FROM photo.artifacts WHERE tenant_id=$1 AND id=$2) FOR UPDATE",
			"SELECT task_id FROM photo.task_attempts WHERE tenant_id=$1 AND task_id=(SELECT created_by_task_id FROM photo.artifacts WHERE tenant_id=$1 AND id=$2) LIMIT 1 FOR UPDATE",
			"SELECT id FROM photo.artifacts WHERE tenant_id=$1 AND id=$2 FOR UPDATE",
			"SELECT id FROM photo.artifacts WHERE tenant_id=$1 AND created_by_task_id=(SELECT created_by_task_id FROM photo.artifacts WHERE tenant_id=$1 AND id=$2) AND output_role='person_report' FOR UPDATE",
			"SELECT id FROM photo.artifacts WHERE tenant_id=$1 AND id=(SELECT params_artifact_id FROM photo.tasks WHERE tenant_id=$1 AND id=(SELECT created_by_task_id FROM photo.artifacts WHERE tenant_id=$1 AND id=$2)) FOR UPDATE",
		} {
			var locked uuid.UUID
			probeErr := personProbe.QueryRow(ctx, query, fixture.Tenant, fixture.PersonMask.ArtifactID).Scan(&locked)
			var postgres *pgconn.PgError
			if !errors.As(probeErr, &postgres) || postgres.Code != "57014" {
				t.Fatal("person authority lock did not block mutation", probeErr)
			}
		}
		for _, item := range []struct {
			query string
			id    uuid.UUID
		}{
			{"SELECT id FROM photo.v2_qualification_policies WHERE id=$2 AND $1::uuid IS NOT NULL FOR UPDATE", fixture.PersonQualification.PolicyID},
			{"SELECT id FROM photo.model_qualifications WHERE tenant_id=$1 AND id=$2 FOR UPDATE", fixture.PersonQualification.QualificationID},
			{"SELECT id FROM photo.artifacts WHERE tenant_id=$1 AND id=$2 FOR UPDATE", fixture.PersonQualification.Report.ArtifactID},
		} {
			var locked uuid.UUID
			probeErr := personProbe.QueryRow(ctx, item.query, fixture.Tenant, item.id).Scan(&locked)
			var postgres *pgconn.PgError
			if !errors.As(probeErr, &postgres) || postgres.Code != "57014" {
				t.Fatal("person qualification lock did not block mutation", probeErr)
			}
		}
		_, phantomErr := personProbe.Exec(ctx, "UPDATE photo.artifacts SET expires_at=NULL WHERE tenant_id=$1 AND id=$2", fixture.Tenant, fixture.AmbiguousPersonMask.ArtifactID)
		var phantomPostgres *pgconn.PgError
		if !errors.As(phantomErr, &phantomPostgres) || phantomPostgres.Code != "57014" {
			t.Fatal("new person candidate crossed inventory barrier", phantomErr)
		}
		t.Log("V27_TRANSFER_PERSON_INVENTORY_BARRIER_OK")
		if _, err := personProbe.Exec(ctx, "RESET statement_timeout"); err != nil {
			t.Fatal(err)
		}
		personProbe.Release()
		t.Log("V27_TRANSFER_PERSON_PRODUCER_CONCURRENT_LOCK_OK")
		evidenceMismatch := personPlan
		evidenceMismatch.person = append([]personProtectionProof(nil), personPlan.person...)
		var evidencePins []personEvidencePin
		if err := json.Unmarshal([]byte(evidenceMismatch.person[0].Evidence), &evidencePins); err != nil || len(evidencePins) != 2 {
			t.Fatal("missing retained evidence pins", err)
		}
		if evidencePins[1].ArtifactID != fixture.PersonReport.ArtifactID {
			t.Fatal("retained report identity mismatch")
		}
		evidencePins[1].Version = "substituted-version"
		changedEvidence, err := canonical.Marshal(evidencePins)
		if err != nil {
			t.Fatal(err)
		}
		evidenceMismatch.person[0].Evidence = string(changedEvidence)
		if err := LockPersonProducerInTransaction(ctx, tx, who, request, evidenceMismatch); err != v2input.ErrConflict {
			t.Fatal("unread report version locked", err)
		}
		qualificationMismatch := personPlan
		qualificationMismatch.person = append([]personProtectionProof(nil), personPlan.person...)
		var qualificationObject personEvidencePin
		if err := json.Unmarshal([]byte(qualificationMismatch.person[0].Qualification), &qualificationObject); err != nil {
			t.Fatal(err)
		}
		if qualificationObject.ArtifactID != fixture.PersonQualification.Report.ArtifactID {
			t.Fatal("qualification pin identity")
		}
		qualificationObject.Version = "unverified-qualification-version"
		changedQualification, err := canonical.Marshal(qualificationObject)
		if err != nil {
			t.Fatal(err)
		}
		qualificationMismatch.person[0].Qualification = string(changedQualification)
		if err := LockPersonProducerInTransaction(ctx, tx, who, request, qualificationMismatch); err != v2input.ErrConflict {
			t.Fatal("unread qualification object locked", err)
		}
		t.Log("V27_TRANSFER_PERSON_QUALIFICATION_LOCK_OK")
		t.Log("V27_TRANSFER_PERSON_EXACT_EVIDENCE_LOCK_OK")
		t.Log("V27_TRANSFER_PERSON_PRODUCER_LOCK_OK")
		t.Log("V27_TRANSFER_PERSON_PLAN_PROOF_OK")
		if _, err := plan.DraftShell(1 << 28); err != nil {
			t.Fatal("physical shell reservation", err)
		}
		admission, err := BeginDraftInTransaction(ctx, tx, who, request, "proposal-publication", first, plan, 1<<28)
		if err != nil || admission.Outcome != "created" || admission.RunID != request.RunID {
			t.Fatal("physical draft admission", admission, err)
		}
		replay, err := BeginDraftInTransaction(ctx, tx, who, request, "proposal-publication", first, plan, 1<<28)
		if err != nil || replay.Outcome != "pending" || replay.RunID != request.RunID {
			t.Fatal("physical draft pending replay", replay, err)
		}

	}
	wrong := first
	wrong.Ref.ArtifactID = uuid.New()
	if err := LockPublishedIntentInTransaction(ctx, tx, who, request, wrong); err == nil {
		t.Fatal("foreign document accepted")
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}

	if fixture.TransferBatch {
		owner, err := pgxpool.New(ctx, os.Getenv("PHOTO_TEST_DATABASE_URL"))
		if err != nil {
			t.Fatal("fixture inspection")
		}
		defer owner.Close()
		probeRegistrationDeadlockRetry(ctx, owner, t)
		var count int
		if err := owner.QueryRow(ctx, "SELECT count(*) FROM photo.runs WHERE tenant_id=$1 AND id=$2", fixture.Tenant, request.RunID).Scan(&count); err != nil || count != 0 {
			t.Fatal("draft rollback", count, err)
		}
		t.Log("V27_PROPOSAL_DRAFT_ROLLBACK_OK")
		var stream, head uuid.UUID
		if err := owner.QueryRow(ctx, "SELECT id,current_document_id FROM photo.v2_document_streams WHERE tenant_id=$1 AND asset_version_id=$2 AND stream_type='draft'", fixture.Tenant, first.Inventory.Targets[0].AssetVersionID).Scan(&stream, &head); err != nil {
			t.Fatal("recipe head", err)
		}
		if _, err := owner.Exec(ctx, "UPDATE photo.v2_document_streams SET current_document_id=NULL WHERE id=$1", stream); err != nil {
			t.Fatal("recipe head mutation", err)
		}
		if _, err := BeginDraft(ctx, scheduler, who, request, "proposal-publication", first, plan, 1<<28); err != v2input.ErrConflict {
			t.Fatal("stale recipe admission", err)
		}
		if err := owner.QueryRow(ctx, "SELECT count(*) FROM photo.runs WHERE tenant_id=$1 AND id=$2", fixture.Tenant, request.RunID).Scan(&count); err != nil || count != 0 {
			t.Fatal("stale recipe persisted draft", count, err)
		}
		if _, err := owner.Exec(ctx, "UPDATE photo.v2_document_streams SET current_document_id=$2 WHERE id=$1", stream, head); err != nil {
			t.Fatal("restore recipe head", err)
		}
		t.Log("V27_PROPOSAL_STALE_RECIPE_REJECTED_OK")
		created, err := BeginDraft(ctx, scheduler, who, request, "proposal-publication", first, plan, 1<<28)
		if err != nil || created.Outcome != "created" || created.RunID != request.RunID {
			t.Fatal("draft commit", created, err)
		}
		// Force a new pool/connection for the durable pending replay.
		resumedPool, err := pgxpool.NewWithConfig(ctx, schedulerCfg.Copy())
		if err != nil {
			t.Fatal("resume pool")
		}
		defer resumedPool.Close()
		resumed, err := BeginDraft(ctx, resumedPool, who, request, "proposal-publication", first, plan, 1<<28)
		if err != nil || resumed.Outcome != "pending" || resumed.RunID != request.RunID {
			t.Fatal("durable pending replay", resumed, err)
		}
		t.Log("V27_PROPOSAL_DRAFT_COMMIT_REPLAY_OK")
		transport := v2input.ScopedPublisher{Documents: artifactjson.Publisher{Repository: artifactjson.SQLRepository{DB: scheduler, Bucket: fixture.Objects.Bucket}, Objects: objects}, Tenant: fixture.Tenant, Run: request.RunID}
		prepared, err := PublishWorkflowTasks(ctx, plan, transport)
		if err != nil || len(prepared) != 1 || prepared[0].Graph.Built.Count < 1 {
			t.Fatal("physical transfer transport", err)
		}
		replayed, err := PublishWorkflowTasks(ctx, plan, transport)
		if err != nil || len(replayed) != 1 || replayed[0].Graph.Built.Manifest != prepared[0].Graph.Built.Manifest {
			t.Fatal("physical transfer transport replay", err)
		}
		graph, err := BindWorkflowTransport(ctx, plan, []v2input.Built{prepared[0].Graph.Built})
		if err != nil || len(graph) != 1 || graph[0].ID != plan.tasks[0].ID {
			t.Fatal("physical frozen graph binding", err)
		}
		invalidProtection := plan
		invalidProtection.protection = append([]protectionProof(nil), plan.protection...)
		invalidProtection.protection[0].Child.SHA256 = strings.Repeat("0", 64)
		invalidTx, err := scheduler.Begin(ctx)
		if err != nil {
			t.Fatal("protection rejection transaction", err)
		}
		defer invalidTx.Rollback(ctx)
		if err := RegisterWorkflowInputsInTransaction(ctx, invalidTx, who, request, first, invalidProtection, []v2input.Built{prepared[0].Graph.Built}); err != v2input.ErrConflict {
			t.Fatal("foreign protection child admitted", err)
		}
		if err := invalidTx.Rollback(ctx); err != nil {
			t.Fatal("protection rejection rollback", err)
		}
		registerTx, err := scheduler.Begin(ctx)
		if err != nil {
			t.Fatal("input registration transaction", err)
		}
		defer registerTx.Rollback(ctx)
		if err := RegisterWorkflowInputsInTransaction(ctx, registerTx, who, request, first, plan, []v2input.Built{prepared[0].Graph.Built}); err != nil {
			t.Fatal("physical input registration", err)
		}
		probe, err := owner.Acquire(ctx)
		if err != nil {
			t.Fatal("protection lock probe", err)
		}
		if _, err := probe.Exec(ctx, "SET statement_timeout='250ms'"); err != nil {
			t.Fatal(err)
		}
		for _, query := range []string{
			"SELECT id FROM photo.tasks WHERE tenant_id=$1 AND id=(SELECT created_by_task_id FROM photo.artifacts WHERE tenant_id=$1 AND id=$2) FOR UPDATE",
			"SELECT id FROM photo.artifacts WHERE tenant_id=$1 AND id=$2 FOR UPDATE",
		} {
			var locked uuid.UUID
			err := probe.QueryRow(ctx, query, fixture.Tenant, fixture.Protection.ArtifactID).Scan(&locked)
			var postgres *pgconn.PgError
			if !errors.As(err, &postgres) || postgres.Code != "57014" {
				t.Fatal("protection lock did not block mutation", err)
			}
		}
		if _, err := probe.Exec(ctx, "RESET statement_timeout"); err != nil {
			t.Fatal(err)
		}
		probe.Release()
		t.Log("V27_PROPOSAL_PROTECTION_LOCKS_OK")
		if err := registerTx.Rollback(ctx); err != nil {
			t.Fatal("registration rollback", err)
		}
		if err := owner.QueryRow(ctx, "SELECT count(*) FROM photo.v2_task_inputs WHERE tenant_id=$1 AND task_id=$2", fixture.Tenant, plan.tasks[0].ID).Scan(&count); err != nil || count != 0 {
			t.Fatal("registration rollback leaked inputs", count, err)
		}
		t.Log("V27_PROPOSAL_INPUT_REGISTRATION_ROLLBACK_OK")
		commitTx, err := scheduler.Begin(ctx)
		if err != nil {
			t.Fatal("registration commit transaction", err)
		}
		defer commitTx.Rollback(ctx)
		if err := RegisterWorkflowInputsInTransaction(ctx, commitTx, who, request, first, plan, []v2input.Built{prepared[0].Graph.Built}); err != nil {
			t.Fatal("registration retry after rollback", err)
		}
		if err := commitTx.Commit(ctx); err != nil {
			t.Fatal("registration commit", err)
		}
		if err := owner.QueryRow(ctx, "SELECT count(*) FROM photo.v2_task_inputs WHERE tenant_id=$1 AND task_id=$2", fixture.Tenant, plan.tasks[0].ID).Scan(&count); err != nil || count != prepared[0].Graph.Built.Count+len(prepared[0].Graph.Built.Chunks)+1 {
			t.Fatal("registered input count", count, err)
		}
		t.Log("V27_PROPOSAL_INPUT_REGISTRATION_COMMIT_OK")
		replayTx, err := resumedPool.Begin(ctx)
		if err != nil {
			t.Fatal("registration replay transaction", err)
		}
		defer replayTx.Rollback(ctx)
		if err := RegisterWorkflowInputsInTransaction(ctx, replayTx, who, request, first, plan, []v2input.Built{prepared[0].Graph.Built}); err != nil {
			t.Fatal("durable registration replay", err)
		}
		if err := replayTx.Commit(ctx); err != nil {
			t.Fatal("registration replay commit", err)
		}
		wrongBuilt := prepared[0].Graph.Built
		wrongBuilt.Manifest.ArtifactID = uuid.New()
		conflictTx, err := scheduler.Begin(ctx)
		if err != nil {
			t.Fatal("registration conflict transaction", err)
		}
		defer conflictTx.Rollback(ctx)
		if err := RegisterWorkflowInputsInTransaction(ctx, conflictTx, who, request, first, plan, []v2input.Built{wrongBuilt}); err != v2input.ErrConflict {
			t.Fatal("changed registration transport", err)
		}
		if err := conflictTx.Rollback(ctx); err != nil {
			t.Fatal("registration conflict rollback", err)
		}
		t.Log("V27_PROPOSAL_INPUT_REGISTRATION_REPLAY_OK")
		preparer := TransferDraftPreparer{Reader: SQLTransferSourceReader{DB: api, Objects: objects, Bucket: fixture.Objects.Bucket, ProjectID: first.Inventory.Scope.ProjectID}, Documents: publisher, Scheduler: resumedPool, TransportDocuments: artifactjson.Publisher{Repository: artifactjson.SQLRepository{DB: resumedPool, Bucket: fixture.Objects.Bucket}, Objects: objects}, Configuration: preparation, ReservedBytes: 1 << 28}
		recovered, err := preparer.Prepare(ctx, who, fixture.Tenant, "proposal-publication", request.Body)
		if err != nil || recovered.RunID != request.RunID || recovered.WorkflowID != request.WorkflowID || recovered.Outcome != "pending" || len(recovered.transports) != 1 || recovered.transports[0].Manifest != prepared[0].Graph.Built.Manifest {
			t.Fatal("integrated draft preparer recovery", err)
		}

		drift := plan
		drift.tasks = plan.DraftTasks()
		drift.tasks[0].ExecutionProfile = []byte(strings.Replace(string(drift.tasks[0].ExecutionProfile), `"backend":"cuda"`, `"backend":"mps"`, 1))
		if _, err := BeginDraft(ctx, resumedPool, who, request, "proposal-publication", first, drift, 1<<28); err != v2input.ErrConflict {
			t.Fatal("registered shell accepted compiler profile drift", err)
		}
		t.Log("V27_PROPOSAL_PREPARER_RECOVERY_OK")
		// Test the ordinary final admission boundary in a caller-owned TX.
		// Rollback preserves this fixture's draft-only external contract.
		sealTx, err := scheduler.Begin(ctx)
		if err != nil {
			t.Fatal("ordinary transfer seal transaction", err)
		}
		defer sealTx.Rollback(context.Background())
		deniedTicket, deniedSealErr := sealTransferDraftInTransaction(ctx, sealTx, who, request, "proposal-publication", first, recovered.plan, recovered.transports)
		if deniedTicket != (v2input.JobTicket{}) || !errors.Is(deniedSealErr, v2input.ErrConflict) {
			t.Fatal("disabled transfer exposed admission", deniedTicket, deniedSealErr)
		}
		if err := sealTx.Rollback(ctx); err != nil {
			t.Fatal("disabled seal rollback", err)
		}
		// Capability enablement is fixture-only; no deployment/model approval is
		// inferred. Keep production's disabled-by-default registry unchanged.
		ownerCfg := cfg.Copy()
		delete(ownerCfg.ConnConfig.RuntimeParams, "role")
		ownerDB, err := pgxpool.NewWithConfig(ctx, ownerCfg)
		if err != nil {
			t.Fatal("fixture capability owner", err)
		}
		defer ownerDB.Close()
		if _, err := ownerDB.Exec(ctx, "UPDATE photo.operation_registry SET v2_enabled=true WHERE operation='semantic.transfer' AND version='1'"); err != nil {
			t.Fatal("fixture capability enablement", err)
		}
		defer ownerDB.Exec(context.Background(), "UPDATE photo.operation_registry SET v2_enabled=false WHERE operation='semantic.transfer' AND version='1'")
		sealTx, err = scheduler.Begin(ctx)
		if err != nil {
			t.Fatal("enabled transfer seal transaction", err)
		}
		defer sealTx.Rollback(context.Background())
		sealedTicket, sealErr := sealTransferDraftInTransaction(ctx, sealTx, who, request, "proposal-publication", first, recovered.plan, recovered.transports)
		if sealErr != nil || sealedTicket.RunID != request.RunID || sealedTicket.WorkflowID != request.WorkflowID || sealedTicket.State != "queued" {
			diagnosticCfg := cfg.Copy()
			delete(diagnosticCfg.ConnConfig.RuntimeParams, "role")
			diagnosticDB, diagnosticErr := pgxpool.NewWithConfig(ctx, diagnosticCfg)
			if diagnosticErr != nil {
				t.Fatal("diagnostic owner pool", diagnosticErr)
			}
			defer diagnosticDB.Close()
			var diagnostic string
			if probeErr := diagnosticDB.QueryRow(ctx, `SELECT jsonb_build_object(
                'status',r.status,'sealed',r.graph_sealed,'cancelled',r.cancel_requested,
                'tenant_setting',current_setting('app.tenant_id',true),
                'expansions',(SELECT jsonb_agg(jsonb_build_object('kind',e.kind,'state',e.state)) FROM photo.graph_expansions e WHERE e.tenant_id=r.tenant_id AND e.run_id=r.id),
                'tasks',(SELECT jsonb_agg(jsonb_build_object('operation',t.operation,'state',t.state,'closure',photo.v2_input_closure_ready(t.id),'enabled',op.v2_enabled,'backend',t.execution_profile->>'backend','backends',op.backends,'resource',t.resource_class,'registry_resource',op.resource_class)) FROM photo.tasks t JOIN photo.operation_registry op ON op.operation=t.operation AND op.version=t.operation_version WHERE t.tenant_id=r.tenant_id AND t.run_id=r.id),
                'quota',(SELECT jsonb_agg(jsonb_build_object('bytes',q.bytes,'state',q.state,'live',q.expires_at>clock_timestamp())) FROM photo.quota_reservations q WHERE q.tenant_id=r.tenant_id AND q.holder_id=r.id)
                )::text FROM photo.runs r WHERE r.tenant_id=$1 AND r.id=$2`, request.TenantID, request.RunID).Scan(&diagnostic); probeErr != nil {
				t.Fatal("ordinary admission diagnostic", probeErr)
			}
			t.Fatal("ordinary transfer complete seal", sealErr, diagnostic)
		}
		// Observe uncommitted effects as the disposable fixture owner; scheduler
		// must retain no direct run_events/outbox/idempotency table access.
		var fixtureOwner string
		if err := ownerDB.QueryRow(ctx, "SELECT current_user").Scan(&fixtureOwner); err != nil || fixtureOwner == "" {
			t.Fatal("fixture observation role", err)
		}
		if _, err := sealTx.Exec(ctx, "SET LOCAL ROLE "+pgx.Identifier{fixtureOwner}.Sanitize()); err != nil {
			t.Fatal("fixture observation role switch", err)
		}
		var queuedEvents int
		if err := sealTx.QueryRow(ctx, "SELECT count(*) FROM photo.run_events WHERE tenant_id=$1 AND run_id=$2 AND type='v2.run.queued'", request.TenantID, request.RunID).Scan(&queuedEvents); err != nil || queuedEvents != 1 {
			t.Fatal("ordinary seal event", queuedEvents, err)
		}
		if err := sealTx.Rollback(ctx); err != nil {
			t.Fatal("ordinary seal rollback", err)
		}
		t.Log("V27_ORDINARY_TRANSFER_SEAL_ROLLBACK_OK")
		if fixture.CommitOrdinary {
			committed, err := preparer.admitPreparedTransfer(ctx, who, request, "proposal-publication", recovered)
			if err != nil || committed != sealedTicket {
				t.Fatal("ordinary committed parent receipt", err)
			}
			restored, err := preparer.Prepare(ctx, who, request.TenantID, "proposal-publication", request.Body)
			if err != nil || restored.Outcome != "replayed" {
				t.Fatal("ordinary durable preparation recovery", err)
			}
			recovered = restored
			for attempt := 0; attempt < 2; attempt++ {
				replayed, err := preparer.replayPreparedTransfer(ctx, who, request, "proposal-publication", recovered)
				if err != nil || replayed != committed {
					t.Fatal("ordinary committed replay", attempt, err)
				}
			}
			alteredWho := who
			alteredWho.SessionID = uuid.New()
			if ticket, err := preparer.replayPreparedTransfer(ctx, alteredWho, request, "proposal-publication", recovered); err == nil || ticket != (v2input.JobTicket{}) {
				t.Fatal("ordinary replay accepted substituted session", err)
			}
			if ticket, err := preparer.replayPreparedTransfer(ctx, who, request, "proposal-publication-other", recovered); err == nil || ticket != (v2input.JobTicket{}) {
				t.Fatal("ordinary replay accepted substituted key", err)
			}
			alteredPrepared := recovered
			alteredPrepared.transports = append([]v2input.Built(nil), recovered.transports...)
			alteredPrepared.transports[0].SHA256 = strings.Repeat("0", 64)
			if ticket, err := preparer.replayPreparedTransfer(ctx, who, request, "proposal-publication", alteredPrepared); err == nil || ticket != (v2input.JobTicket{}) {
				t.Fatal("ordinary replay accepted substituted transport", err)
			}
			var events, messages, receipts int
			if err := ownerDB.QueryRow(ctx, `SELECT
                (SELECT count(*) FROM photo.run_events WHERE tenant_id=$1 AND run_id=$2 AND type='v2.run.queued'),
                (SELECT count(*) FROM photo.outbox WHERE tenant_id=$1 AND payload->>'run_id'=$2::text AND routing_key='v2.run.queued'),
                (SELECT count(*) FROM photo.idempotency_records WHERE tenant_id=$1 AND resource_id=$2 AND route='V2.Admit' AND response_status=202)`, request.TenantID, request.RunID).Scan(&events, &messages, &receipts); err != nil || events != 1 || messages != 1 || receipts != 1 {
				t.Fatal("ordinary durable effects", events, messages, receipts, err)
			}
			t.Log("V27_ORDINARY_TRANSFER_COMMITTED_OK")
		}

		t.Log("V27_PROPOSAL_TRANSPORT_PUBLICATION_OK")

	}
	fixture.Intent.Strength = .9
	changed, _ := canonical.Marshal(fixture.Intent)
	changedRequest, err := CompileRequest(fixture.Tenant, fixture.Actor, "proposal-publication", changed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PublishIntent(ctx, publisher, who, changedRequest); err == nil {
		t.Fatal("changed proposal accepted")
	}
	t.Log("V27_PROPOSAL_PUBLICATION_OK")
}

type afterIntentPut struct {
	v2draft.ImmutableStore
	after func() error
}

func (s afterIntentPut) PutImmutable(ctx context.Context, key string, body io.Reader, size int64, digest, media string) (objectstore.Object, error) {
	object, err := s.ImmutableStore.PutImmutable(ctx, key, body, size, digest, media)
	if err == nil {
		err = s.after()
	}
	return object, err
}

// Trigger at a selected matching object open; the default is the second open.
type afterSecondMaskOpen struct {
	delegate v2input.SessionAnalysisObjectReader
	key      string
	opens    int
	at       int
	done     bool
	after    func(context.Context) error
}

func (s *afterSecondMaskOpen) Open(ctx context.Context, object objectstore.Object) (io.ReadCloser, error) {
	stream, err := s.delegate.Open(ctx, object)
	if err != nil {
		return nil, err
	}
	if object.Key == s.key {
		s.opens++
		at := s.at
		if at == 0 {
			at = 2
		}
		if s.opens == at {
			s.done = true
			if err := s.after(ctx); err != nil {
				stream.Close()
				return nil, err
			}
		}
	}
	return stream, nil
}

// Mutates transport bytes while keeping the catalog/version/hash pins intact.
type corruptDefectOpen struct {
	delegate v2input.SessionAnalysisObjectReader
	key      string
	done     bool
}

func (s *corruptDefectOpen) Open(ctx context.Context, object objectstore.Object) (io.ReadCloser, error) {
	stream, err := s.delegate.Open(ctx, object)
	if err != nil || object.Key != s.key {
		return stream, err
	}
	body, readErr := io.ReadAll(stream)
	closeErr := stream.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("empty corrupt fixture")
	}
	body[len(body)-1] ^= 1
	s.done = true
	return io.NopCloser(bytes.NewReader(body)), nil
}

// Test-only revocation between a successful lookup and final qualification check.
type afterDefectProfileLookup struct {
	delegate v2draft.ArtifactCatalog
	after    func(context.Context) error
	done     bool
}

func (c *afterDefectProfileLookup) Lookup(ctx context.Context, who auth.Identity, tenant uuid.UUID, node v2draft.LayerRef) (objectstore.Object, error) {
	object, err := c.delegate.Lookup(ctx, who, tenant, node)
	if err != nil {
		return objectstore.Object{}, err
	}
	if !c.done {
		c.done = true
		if err := c.after(ctx); err != nil {
			return objectstore.Object{}, err
		}
	}
	return object, nil
}
