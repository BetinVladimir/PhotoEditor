package semanticproposal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/masknpy"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
	"photoeditor/backend/internal/v2workflow"
)

type nativeDeniedObjects struct{ calls int }

func (o *nativeDeniedObjects) Open(context.Context, objectstore.Object) (io.ReadCloser, error) {
	o.calls++
	return nil, errors.New("unexpected object IO in denied tenant scope")
}

func TestIntegrationNativeDefectContent(t *testing.T) {
	raw, taskRaw := os.Getenv("PHOTO_SEMANTIC_INTENT_PUBLISH_CASE"), os.Getenv("PHOTO_TEST_V27_GATEWAY_TASK")
	if raw == "" || taskRaw == "" {
		t.Skip("owned native worker fixture required")
	}
	var fixture struct {
		Tenant, Actor, Session, Project uuid.UUID
		Intent                          v2input.SemanticEditIntent
		NativeIntent, NativeSnapshot    v2input.Ref
		NativeSelectionOverlap          bool
		NativeSelectionCount            int64
		Objects                         objectstore.Config
		Qualification                   v2input.SemanticQualificationPin
		RequiredProtection              v2input.Ref
		PersonQualification             v2input.SemanticQualificationPin
		DefectQualification             v2input.SemanticQualificationPin
		FaceQualifications              [2]v2input.SemanticQualificationPin
		ParseQualification              v2input.SemanticQualificationPin
		PersonMask                      v2input.Ref
		NativeForeign                   struct {
			Tenant, Actor, Session, Project uuid.UUID
			Reference                       v2input.SessionVersionBinding
		}
	}
	task, err := uuid.Parse(taskRaw)
	if err != nil || json.Unmarshal([]byte(raw), &fixture) != nil {
		t.Fatal("invalid native content fixture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	cfg, err := pgxpool.ParseConfig(os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal("database configuration")
	}
	cfg.ConnConfig.RuntimeParams["role"] = "photo_api"
	db, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal("database unavailable")
	}
	defer db.Close()
	objects, err := objectstore.New(fixture.Objects)
	if err != nil {
		t.Fatal("object configuration")
	}
	reader := SQLTransferSourceReader{DB: db, Objects: objects, Bucket: fixture.Objects.Bucket, ProjectID: fixture.Project, ManualRasterLimits: ManualRasterVerificationLimits{PointChecks: 10000000, EdgeEvaluations: 100000000, PayloadBytes: 2000000}, ManualRasterBatchLimits: ManualRasterVerificationLimits{PointChecks: 100000000, EdgeEvaluations: 1000000000, PayloadBytes: 20000000}}
	who := auth.Identity{UserID: fixture.Actor, SessionID: fixture.Session}
	verified, err := reader.ReadNativeDefectResultEvidence(ctx, who, fixture.Tenant, fixture.Intent.Reference, task)
	if err != nil || verified.TaskID != task || verified.ResultID == uuid.Nil || verified.Evidence.LabelPNG.ArtifactID == uuid.Nil || len(verified.Evidence.Report) == 0 {
		t.Fatal("native worker content verification", err)
	}
	if len(verified.Faces) != 1 || len(verified.MaskRoots) != 1 {
		t.Fatal("native result missing verified input provenance")
	}
	schedulerConfig := cfg.Copy()
	schedulerConfig.ConnConfig.RuntimeParams["role"] = "photo_scheduler"
	scheduler, err := pgxpool.NewWithConfig(ctx, schedulerConfig)
	if err != nil {
		t.Fatal("scheduler database unavailable")
	}
	defer scheduler.Close()
	for _, changed := range []bool{false, true} {
		tx, err := scheduler.Begin(ctx)
		if err != nil {
			t.Fatal("native lock transaction", err)
		}
		proof := verified.admissionProof
		if changed {
			proof.catalog.ResultID = uuid.New()
		}
		lockErr := reader.lockNativeDefectResult(ctx, tx, who, fixture.Tenant, fixture.Intent.Reference, proof)
		rollbackErr := tx.Rollback(ctx)
		if rollbackErr != nil || (!changed && lockErr != nil) || (changed && lockErr == nil) {
			t.Fatal("native canonical result lock", changed, lockErr, rollbackErr)
		}
	}
	t.Log("V27_NATIVE_DEFECT_RESULT_TRANSACTION_LOCK_OK")
	body, err := json.Marshal(fixture.Intent)
	if err != nil {
		t.Fatal(err)
	}
	request, err := CompileRequest(fixture.Tenant, fixture.Actor, "proposal-publication", body)
	if err != nil {
		t.Fatal(err)
	}
	bound, err := reader.ReadNativeDefectResultForRequest(ctx, who, request, "proposal-publication", fixture.NativeIntent, fixture.NativeSnapshot, task)
	if err != nil || bound.ResultID != verified.ResultID {
		t.Fatal("native parent binding", err)
	}
	tx, err := scheduler.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	lockErr := reader.lockNativeDefectContinuation(ctx, tx, who, fixture.Tenant, fixture.Intent.Reference, bound.parentProof)
	rollbackErr := tx.Rollback(ctx)
	if lockErr != nil || rollbackErr != nil {
		t.Fatal("native parent continuation lock", lockErr, rollbackErr)
	}
	other, err := CompileRequest(fixture.Tenant, fixture.Actor, "different-parent", body)
	if err != nil {
		t.Fatal(err)
	}
	spy := &nativeDeniedObjects{}
	scoped := reader
	scoped.Objects = spy
	denied, err := scoped.ReadNativeDefectResultForRequest(ctx, who, other, "different-parent", fixture.NativeIntent, fixture.NativeSnapshot, task)
	if err == nil || spy.calls != 0 || denied.ResultID != uuid.Nil {
		t.Fatal("different native parent accepted", err, spy.calls)
	}
	t.Log("V27_NATIVE_DEFECT_PARENT_BINDING_OK")
	preparedProtection, err := reader.prepareNativeDefectProtection(ctx, who, request, "proposal-publication", fixture.NativeIntent, fixture.NativeSnapshot, task)
	if err != nil || preparedProtection.document == uuid.Nil || preparedProtection.body == "" || preparedProtection.pixels != bound.Pins.Protection || preparedProtection.parent != bound.parentProof {
		t.Fatal("native protection publication preparation", err)
	}
	t.Log("V27_NATIVE_DEFECT_PROTECTION_DOCUMENT_PREPARED_OK")
	imageBody, err := reader.Read(ctx, who, fixture.Tenant, fixture.Intent.Reference)
	if err != nil {
		t.Fatal(err)
	}
	publisher := v2draft.Publisher{DB: db, Objects: objects, Bucket: fixture.Objects.Bucket}
	root, err := publisher.PublishSemanticProtectionDocument(ctx, who, fixture.Tenant, preparedProtection.document, fixture.Intent.Reference, imageBody, []byte(preparedProtection.body))
	if err != nil || root.ArtifactID == uuid.Nil {
		t.Fatal("native protection immutable publication", err)
	}
	replay, err := publisher.PublishSemanticProtectionDocument(ctx, who, fixture.Tenant, preparedProtection.document, fixture.Intent.Reference, imageBody, []byte(preparedProtection.body))
	if err != nil || replay != root {
		t.Fatal("native protection publication replay", err)
	}
	// Until native provenance registration exists, publication must not make a
	// backend-created document look like a canonical worker-produced mask root.
	if err := reader.VerifyProtectionOrigin(ctx, who, fixture.Tenant, fixture.Intent.Reference, root, preparedProtection.pixels); err == nil {
		t.Fatal("unregistered derived protection origin accepted")
	}
	t.Log("V27_NATIVE_DEFECT_PROTECTION_DOCUMENT_PUBLICATION_OK")
	for replay := 0; replay < 2; replay++ {
		if err := reader.registerNativeDefectProtection(ctx, scheduler, who, fixture.Tenant, fixture.Intent.Reference, preparedProtection, root); err != nil {
			t.Fatal("native protection provenance registration", replay, err)
		}
	}
	changedPreparation := preparedProtection
	changedPreparation.report += " "
	if err := reader.registerNativeDefectProtection(ctx, scheduler, who, fixture.Tenant, fixture.Intent.Reference, changedPreparation, root); err == nil {
		t.Fatal("changed native report bytes registered")
	}
	t.Log("V27_NATIVE_DEFECT_PROTECTION_REGISTRATION_OK")
	registeredProtection, err := reader.ReadRegisteredNativeDefectProtection(ctx, who, request, "proposal-publication", fixture.NativeIntent, fixture.NativeSnapshot, task, root)
	if err != nil || registeredProtection.Root != root || registeredProtection.Pixels != preparedProtection.pixels || string(registeredProtection.Body) != preparedProtection.body {
		t.Fatal("registered native protection read", err)
	}
	t.Log("V27_NATIVE_DEFECT_REGISTERED_PROTECTION_READ_OK")
	coordinated, err := reader.PublishRegisteredNativeDefectProtection(ctx, who, fixture.Tenant, "proposal-publication", body, fixture.NativeIntent, fixture.NativeSnapshot, task, publisher, scheduler)
	if err != nil || coordinated.Root != root || coordinated.Pixels != preparedProtection.pixels || string(coordinated.Body) != preparedProtection.body {
		t.Fatal("native protection committed coordinator replay", err)
	}
	t.Log("V27_NATIVE_DEFECT_PROTECTION_COORDINATOR_OK")
	separated, selectionProof, err := reader.verifyNativeSelectionSeparation(ctx, who, request, "proposal-publication", fixture.NativeIntent, fixture.NativeSnapshot, task, root)
	if fixture.NativeSelectionCount < 1 {
		t.Fatal("independent NPY selection oracle required")
	}
	if fixture.NativeSelectionOverlap {
		if !errors.Is(err, masknpy.ErrInvalid) || separated != 0 || selectionProof != (nativeSelectionSeparationProof{}) {
			t.Fatal("native protected overlap accepted", separated, err)
		}
		t.Log("V27_NATIVE_DEFECT_SELECTION_OVERLAP_REFUSED_OK")
	} else if err != nil || separated != fixture.NativeSelectionCount {
		t.Fatal("native selected protection separation", separated, err)
	}
	if !fixture.NativeSelectionOverlap {
		if selectionProof.count != separated || selectionProof.root != request.Intent.ReferenceMask {
			t.Fatal("missing private selection proof")
		}
		for changed := 0; changed < 2; changed++ {
			retained := selectionProof
			if changed == 1 {
				retained.pixelsObject.VersionID += "-changed"
			}
			tx, err := scheduler.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			lockErr := reader.lockNativeSelectionSeparation(ctx, tx, who, request, retained)
			_ = tx.Rollback(ctx)
			if changed == 0 && lockErr != nil {
				t.Fatal("native selection TX lock", lockErr)
			}
			if changed == 1 && lockErr == nil {
				t.Fatal("changed selection object version accepted")
			}
		}
		t.Log("V27_NATIVE_DEFECT_SELECTION_TRANSACTION_LOCK_OK")
	}
	t.Log("V27_NATIVE_DEFECT_SELECTION_SEPARATION_OK")
	personQ := fixture.PersonQualification
	personExpected := v2qualification.Expected{Operation: "mask.person", Version: "1", Backend: "cuda", ModelLockSHA: personQ.ModelLockSHA, RuntimeLockSHA: personQ.RuntimeLockSHA, DeviceProfileSHA: personQ.DeviceProfileSHA, DatasetSHA: personQ.DatasetSHA, IssuerID: personQ.IssuerID, RequiredChecks: []string{"quality"}, TestFixtureSHAs: map[string]struct{}{strings.Repeat("e", 64): {}}, EvidenceSHAs: map[string]struct{}{strings.Repeat("f", 64): {}}, CalibrationSHAs: map[string]struct{}{personQ.Calibration.SHA256: {}}, LicenseCleared: true}
	coveredRoot, covered, personProof, err := reader.verifyNativePersonProtectionCoverage(ctx, who, request, "proposal-publication", fixture.NativeIntent, fixture.NativeSnapshot, task, root, personQ.Report, personExpected)
	if err != nil || coveredRoot != fixture.PersonMask || covered != 1 {
		t.Fatal("qualified native person coverage", covered, err)
	}
	t.Log("V27_NATIVE_DEFECT_QUALIFIED_PERSON_COVERAGE_OK")
	for changed := 0; changed < 2; changed++ {
		retained := personProof
		if changed == 1 {
			retained.evidence = "[]"
		}
		tx, err := scheduler.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		lockErr := reader.lockNativePersonProtection(ctx, tx, who, fixture.Tenant, fixture.Intent.Reference, retained)
		rollbackErr := tx.Rollback(ctx)
		if rollbackErr != nil || (changed == 0 && lockErr != nil) || (changed == 1 && lockErr == nil) {
			t.Fatal("native qualified person transaction lock", changed, lockErr, rollbackErr)
		}
	}
	t.Log("V27_NATIVE_DEFECT_QUALIFIED_PERSON_TRANSACTION_LOCK_OK")
	var originalReference *nativeReferenceProtectionProof
	if !fixture.NativeSelectionOverlap {
		combined, err := reader.prepareNativeReferenceProtection(ctx, who, request, "proposal-publication", fixture.NativeIntent, fixture.NativeSnapshot, task, root, personQ.Report, personExpected)
		if err != nil || combined.selection.count != fixture.NativeSelectionCount || combined.person.root != fixture.PersonMask {
			t.Fatal("composed native reference protection", err)
		}
		retainedReference := combined
		originalReference = &retainedReference
		for changed := 0; changed < 5; changed++ {
			retained := combined
			if changed == 1 {
				retained.person.registered.objects[0].object.VersionID += "-foreign"
			}
			if changed == 2 {
				retained.person.evidence = "[]"
			}
			if changed == 3 {
				retained.policy.body = "[]"
			}
			if changed == 4 {
				retained.policySHA = strings.Repeat("0", 64)
			}
			tx, err := scheduler.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			lockErr := reader.lockNativeReferenceProtection(ctx, tx, who, request, retained)
			rollbackErr := tx.Rollback(ctx)
			if rollbackErr != nil || (changed == 0 && lockErr != nil) || (changed > 0 && lockErr == nil) {
				t.Fatal("composed native reference TX lock", changed, lockErr, rollbackErr)
			}
		}
		func() {
			ownerCfg := cfg.Copy()
			delete(ownerCfg.ConnConfig.RuntimeParams, "role")
			owner, err := pgxpool.NewWithConfig(ctx, ownerCfg)
			if err != nil {
				t.Fatal("policy owner unavailable")
			}
			defer owner.Close()
			var rows []qualificationPolicyRow
			if json.Unmarshal([]byte(combined.policy.body), &rows) != nil {
				t.Fatal("retained policy body")
			}
			policyID := uuid.Nil
			for _, row := range rows {
				if row.Operation == "face.parse" && row.Admission && row.Enabled {
					if policyID != uuid.Nil {
						t.Fatal("ambiguous parser policy")
					}
					policyID = row.ID
				}
			}
			if policyID == uuid.Nil {
				t.Fatal("parser policy required")
			}
			tag, err := owner.Exec(ctx, "UPDATE photo.v2_qualification_policies SET enabled=false,admission_enabled=false WHERE id=$1 AND enabled AND admission_enabled", policyID)
			if err != nil || tag.RowsAffected() != 1 {
				t.Fatal("policy revocation", err)
			}
			restored := false
			restore := func() {
				cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				tag, err := owner.Exec(cleanup, "UPDATE photo.v2_qualification_policies SET enabled=true,admission_enabled=true WHERE id=$1", policyID)
				if err != nil || tag.RowsAffected() != 1 {
					t.Error("policy restoration", err)
					return
				}
				restored = true
			}
			defer func() {
				if !restored {
					restore()
				}
			}()
			denied := new(nativeDeniedObjects)
			revokedReader := reader
			revokedReader.Objects = denied
			rejected, err := revokedReader.prepareNativeReferenceProtection(ctx, who, request, "proposal-publication", fixture.NativeIntent, fixture.NativeSnapshot, task, root, personQ.Report, personExpected)
			if err == nil || rejected != (nativeReferenceProtectionProof{}) || denied.calls != 0 {
				t.Fatal("revoked policy permitted object IO", err, denied.calls)
			}
			tx, err := scheduler.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			lockErr := reader.lockNativeReferenceProtection(ctx, tx, who, request, combined)
			rollbackErr := tx.Rollback(ctx)
			if lockErr == nil || rollbackErr != nil {
				t.Fatal("stale policy proof accepted", lockErr, rollbackErr)
			}
			restore()
			if !restored {
				t.Fatal("policy not restored")
			}
			tx, err = scheduler.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			lockErr = reader.lockNativeReferenceProtection(ctx, tx, who, request, combined)
			rollbackErr = tx.Rollback(ctx)
			if lockErr != nil || rollbackErr != nil {
				t.Fatal("restored policy rejected", lockErr, rollbackErr)
			}
			t.Log("V27_NATIVE_DEFECT_POLICY_REVOCATION_AND_RECOVERY_OK")
		}()
		t.Log("V27_NATIVE_DEFECT_REFERENCE_PROTECTION_TRANSACTION_LOCK_OK")
	}

	for changed := 0; changed < 5; changed++ {
		alteredReader, alteredWho, alteredTenant, alteredBinding, alteredTask := reader, who, fixture.Tenant, fixture.Intent.Reference, task
		switch changed {
		case 0:
			alteredTenant = uuid.New()
		case 1:
			alteredReader.ProjectID = uuid.New()
		case 2:
			alteredWho.UserID = uuid.New()
		case 3:
			alteredBinding.AssetVersionID = uuid.New()
		case 4:
			alteredTask = uuid.New()
		}
		denied, err := alteredReader.ReadNativeDefectResultEvidence(ctx, alteredWho, alteredTenant, alteredBinding, alteredTask)
		if err == nil || denied.TaskID != uuid.Nil || denied.ResultID != uuid.Nil || len(denied.Evidence.Report) != 0 || len(denied.Evidence.Params) != 0 {
			t.Fatal("native scope substitution accepted", changed, err)
		}
	}

	foreign := fixture.NativeForeign
	if foreign.Tenant == uuid.Nil || foreign.Tenant == fixture.Tenant || foreign.Actor == uuid.Nil || foreign.Actor == fixture.Actor || foreign.Session == uuid.Nil || foreign.Project == uuid.Nil || foreign.Project == fixture.Project || foreign.Reference.AssetVersionID == uuid.Nil || foreign.Reference.AssetVersionID == fixture.Intent.Reference.AssetVersionID {
		t.Fatal("real distinct tenant fixture required")
	}
	foreignWho := auth.Identity{UserID: foreign.Actor, SessionID: foreign.Session}
	cases := []struct {
		name            string
		who             auth.Identity
		tenant, project uuid.UUID
		binding         v2input.SessionVersionBinding
	}{
		{"foreign owner with foreign scope", foreignWho, foreign.Tenant, foreign.Project, foreign.Reference},
		{"shared member with foreign scope", who, foreign.Tenant, foreign.Project, foreign.Reference},
		{"foreign project", who, fixture.Tenant, foreign.Project, fixture.Intent.Reference},
		{"foreign source version", who, fixture.Tenant, fixture.Project, foreign.Reference},
		{"foreign actor", foreignWho, fixture.Tenant, fixture.Project, fixture.Intent.Reference},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			spy := &nativeDeniedObjects{}
			scoped := reader
			scoped.ProjectID, scoped.Objects = c.project, spy
			denied, err := scoped.ReadNativeDefectResultEvidence(ctx, c.who, c.tenant, c.binding, task)
			if err == nil || spy.calls != 0 || denied.TaskID != uuid.Nil || denied.ResultID != uuid.Nil || len(denied.Evidence.Report) != 0 || len(denied.Evidence.Params) != 0 || len(denied.Faces) != 0 || len(denied.MaskRoots) != 0 {
				t.Fatal("cross-tenant result exposed or object IO attempted", err, spy.calls)
			}
		})
	}
	t.Log("V27_NATIVE_DEFECT_REAL_TENANT_ISOLATION_OK")
	for _, role := range []string{"photo_api", "photo_worker", "photo_scheduler", "photo_gateway", "photo_portal", "photo_publisher", "photo_maintenance"} {
		var granted bool
		if err := db.QueryRow(ctx, "SELECT has_function_privilege($1,'photo.semantic_defect_declares_input(uuid,uuid,uuid,text)','EXECUTE')", role).Scan(&granted); err != nil || granted {
			t.Fatal("native declaration helper exposed", role, err)
		}
	}
	t.Log("V27_NATIVE_DEFECT_WORKER_PROVENANCE_SCOPE_OK")

	t.Run("qualified-original-native-producers", func(t *testing.T) {
		qualificationCtx, qualificationCancel := context.WithTimeout(context.Background(), 180*time.Second)
		defer qualificationCancel()
		color, err := LoadTransferColorProfile("../../../resources/icc")
		if err != nil {
			t.Fatal("deployed ICC registry", err)
		}
		reports := nativeDefectQualificationReports{Defect: fixture.DefectQualification.Report,
			Faces:  [2]v2input.Ref{fixture.FaceQualifications[0].Report, fixture.FaceQualifications[1].Report},
			Parser: [2]v2input.Ref{fixture.ParseQualification.Report, fixture.FaceQualifications[1].Report}}
		var originalParams struct {
			Faces []struct {
				ManualMode bool            `json:"manual_mode"`
				Skin       json.RawMessage `json:"skin"`
			} `json:"faces"`
		}
		if json.Unmarshal(verified.Evidence.Params, &originalParams) != nil || len(originalParams.Faces) != 1 || !originalParams.Faces[0].ManualMode || (len(originalParams.Faces[0].Skin) != 0 && string(originalParams.Faces[0].Skin) != "null") {
			t.Fatal("manual native fixture declaration changed; requires separate skin scenario")
		}
		proof, err := reader.prepareQualifiedNativeDefectResult(qualificationCtx, who, request, "proposal-publication", fixture.NativeIntent, fixture.NativeSnapshot, task, reports, color)
		if err != nil || !proof.verified || proof.faceCount != 1 || proof.parserCount != 0 {
			t.Fatal("qualified original native producers", err, proof.faceCount, proof.parserCount)
		}
		for _, mode := range []string{"original", "missing-landmarks", "missing-parser-declaration", "missing-parser-report", "changed-landmark-report", "changed-detector"} {
			candidate := proof
			switch mode {
			case "missing-landmarks":
				candidate.faces = nil
				candidate.faceCount = 0
			case "missing-parser-report":
				candidate.reports.reports[2] = personEvidenceObject{}
			case "missing-parser-declaration":
				candidate.parserSet = ""
			case "changed-landmark-report":
				candidate.faces = append([]facePreparationProof(nil), proof.faces...)
				candidate.faces[0].qualification[0].ref.ArtifactID = uuid.New()
			case "changed-detector":
				candidate.faces = append([]facePreparationProof(nil), proof.faces...)
				candidate.faces[0].objects[1][0].ref.ArtifactID = uuid.New()
			}
			tx, err := scheduler.Begin(qualificationCtx)
			if err != nil {
				t.Fatal(err)
			}
			lockErr := reader.lockQualifiedNativeDefectResult(qualificationCtx, tx, who, request, candidate)
			rollbackErr := tx.Rollback(qualificationCtx)
			if rollbackErr != nil || (mode == "original" && lockErr != nil) || (mode != "original" && lockErr == nil) {
				t.Fatal("qualified native TX", mode, lockErr, rollbackErr)
			}
		}
		if originalReference != nil {
			continuation, err := composeNativeReferenceContinuation(proof, *originalReference)
			if err != nil {
				t.Fatal("original native continuation composition", err)
			}
			for _, mode := range []string{"original", "mixed-result", "missing-person-evidence"} {
				candidate := continuation
				switch mode {
				case "mixed-result":
					candidate.defect.parent.result.catalog.ResultID = uuid.New()
				case "missing-person-evidence":
					candidate.protection.person.evidence = ""
				}
				tx, err := scheduler.Begin(qualificationCtx)
				if err != nil {
					t.Fatal(err)
				}
				lockErr := reader.lockNativeReferenceContinuation(qualificationCtx, tx, who, request, candidate)
				rollbackErr := tx.Rollback(qualificationCtx)
				if rollbackErr != nil || (mode == "original" && lockErr != nil) || (mode != "original" && lockErr == nil) {
					t.Fatal("native continuation TX", mode, lockErr, rollbackErr)
				}
			}
			sourceMetadata, sourceProof, err := reader.readQualifiedSourcePreparationProof(qualificationCtx, who, fixture.Tenant, request.Intent.Reference, color, false)
			if err != nil {
				t.Fatal("native transfer reference source", err)
			}
			sourceContinuation, err := composeNativeSourceContinuation(sourceProof, continuation, color)
			if err != nil {
				t.Fatal("native source continuation composition", err)
			}
			for _, mode := range []string{"original", "changed-pixel-version", "missing-person-evidence"} {
				candidate := sourceContinuation
				switch mode {
				case "changed-pixel-version":
					sourcePins, err := decodeSourcePreparationPins(candidate.source)
					if err != nil {
						t.Fatal(err)
					}
					sourcePins[1].Object.VersionID += "-foreign"
					body, err := canonical.Marshal(sourcePins)
					if err != nil {
						t.Fatal(err)
					}
					candidate.source.pins = string(body)
				case "missing-person-evidence":
					candidate.native.protection.person.evidence = ""
				}
				tx, err := scheduler.Begin(qualificationCtx)
				if err != nil {
					t.Fatal(err)
				}
				lockErr := reader.lockNativeSourceContinuation(qualificationCtx, tx, who, request, candidate, color)
				rollbackErr := tx.Rollback(qualificationCtx)
				if rollbackErr != nil || (mode == "original" && lockErr != nil) || (mode != "original" && lockErr == nil) {
					t.Fatal("native source continuation TX", mode, lockErr, rollbackErr)
				}
			}
			t.Log("V27_NATIVE_DEFECT_SOURCE_CONTINUATION_TRANSACTION_LOCK_OK")
			preparedMetadata, preparedSource, err := reader.prepareNativeSourceContinuation(qualificationCtx, who, request, "proposal-publication", fixture.NativeIntent, fixture.NativeSnapshot, task, root, reports, TransferPersonProtection{Report: personQ.Report, Expected: personExpected}, color)
			if err != nil || preparedSource.source != sourceProof || preparedSource.native.defect.parent != sourceContinuation.native.defect.parent || preparedSource.native.defect.policy != sourceContinuation.native.defect.policy || preparedMetadata.Width != sourceMetadata.Width || preparedMetadata.Height != sourceMetadata.Height || preparedMetadata.Gamma != sourceMetadata.Gamma {
				t.Fatal("actual native source preparation", err)
			}
			for _, changed := range []bool{false, true} {
				candidate := preparedSource
				if changed {
					candidate.native.protection.person.evidence = ""
				}
				tx, err := scheduler.Begin(qualificationCtx)
				if err != nil {
					t.Fatal(err)
				}
				lockErr := reader.lockNativeSourceContinuation(qualificationCtx, tx, who, request, candidate, color)
				rollbackErr := tx.Rollback(qualificationCtx)
				if rollbackErr != nil || (!changed && lockErr != nil) || (changed && lockErr == nil) {
					t.Fatal("prepared native source TX", changed, lockErr, rollbackErr)
				}
			}
			t.Log("V27_NATIVE_DEFECT_SOURCE_PREPARATION_TRANSACTION_LOCK_OK")
			pinnedSources, err := decodeSourcePreparationPins(preparedSource.source)
			if err != nil {
				t.Fatal(err)
			}
			docBudget := pinnedSources[0].Object.Bytes + preparedSource.native.protection.selection.rootObject.Bytes + preparedSource.native.protection.selection.registered.objects[0].object.Bytes
			documents, err := reader.readNativeTransferReferenceDocuments(qualificationCtx, who, request, preparedSource, color, docBudget)
			if err != nil {
				t.Fatal("native reference compiler documents", err)
			}
			for _, pin := range []struct {
				body json.RawMessage
				ref  v2input.Ref
			}{{documents.reference, request.Intent.Reference.Source}, {documents.selected, request.Intent.ReferenceMask}, {documents.protection, root}} {
				digest := sha256.Sum256(pin.body)
				if hex.EncodeToString(digest[:]) != pin.ref.SHA256 {
					t.Fatal("original document bytes replaced")
				}
			}
			documentTx, err := scheduler.Begin(qualificationCtx)
			if err != nil {
				t.Fatal(err)
			}
			documentLockErr := reader.lockNativeSourceContinuation(qualificationCtx, documentTx, who, request, documents.proof, color)
			documentRollbackErr := documentTx.Rollback(qualificationCtx)
			if documentLockErr != nil || documentRollbackErr != nil {
				t.Fatal("document retained proof TX", documentLockErr, documentRollbackErr)
			}
			for _, mode := range []string{"short-budget", "changed-selection-version"} {
				spy := &nativeDeniedObjects{}
				scoped := reader
				scoped.Objects = spy
				candidate := preparedSource
				budget := docBudget
				if mode == "short-budget" {
					budget--
				} else {
					candidate.native.protection.selection.rootObject.VersionID += "-foreign"
				}
				denied, err := scoped.readNativeTransferReferenceDocuments(qualificationCtx, who, request, candidate, color, budget)
				if err == nil || spy.calls != 0 || len(denied.reference) != 0 || len(denied.selected) != 0 || len(denied.protection) != 0 || denied.proof.native.defect.verified {
					t.Fatal("denied document reader exposed bytes or attempted IO", mode, err, spy.calls)
				}
			}
			t.Log("V27_NATIVE_DEFECT_TRANSFER_REFERENCE_DOCUMENTS_OK")
			published, err := PublishIntent(qualificationCtx, publisher, who, request)
			if err != nil {
				t.Fatal("native transfer intent publication", err)
			}
			policies, err := selectQualificationPolicies(preparedSource.native.defect.policy, []string{"semantic.transfer"})
			if err != nil {
				t.Fatal(err)
			}
			configuration := TransferPreparation{Qualification: fixture.Qualification, Expected: policies["semantic.transfer"], Color: color, Costs: v2input.SemanticQualifiedCosts{RAMBytes: 101, VRAMBytes: 202, ScratchBytes: 303}, RAM: 1 << 30, VRAM: 1 << 30, Scratch: 1 << 30, Output: 1 << 30, MetadataBytes: 64 << 20, PersonProtection: &TransferPersonProtection{Report: personQ.Report, Expected: personExpected}, SourceProtection: root, TargetProtection: []v2input.Ref{fixture.RequiredProtection}}
			nativeInputs, nativeDocuments, err := reader.prepareNativeTransferInputs(qualificationCtx, who, request, published, configuration, documents)
			if err != nil || len(nativeInputs) != 1 || len(nativeDocuments) != 1 || nativeInputs[0].nativeReference == nil || nativeInputs[0].sourcePerson != nil || nativeInputs[0].targetPerson == nil {
				t.Fatal("native target batch preparation", err)
			}
			parameterRefs, err := PublishTransferParams(qualificationCtx, publisher, who, request, "proposal-publication", published, fixture.Qualification.ModelLockSHA, fixture.Qualification.Calibration)
			if err != nil {
				t.Fatal("native transfer Params publication", err)
			}
			transferSnapshot, transferSnapshotBody, err := reader.PublishTargetSnapshot(qualificationCtx, publisher, who, request, "proposal-publication", published)
			if err != nil {
				t.Fatal("native transfer snapshot publication", err)
			}
			nativePlan, err := CompileTransferWorkflowPlanFromDocuments(request, who.SessionID, published, transferSnapshot, transferSnapshotBody, parameterRefs, nativeInputs, fixture.Qualification, nativeDocuments)
			if err != nil || !nativePlan.nativeReferenceRequired || nativePlan.nativeReference == nil || len(nativePlan.person) != 1 || len(nativePlan.protection) != 2 {
				t.Fatal("native transfer workflow compiler", err)
			}
			for _, mode := range []string{"original", "missing-native-proof", "missing-target-person", "changed-native-pixel-version"} {
				candidate := nativePlan
				switch mode {
				case "missing-native-proof":
					candidate.nativeReference = nil
				case "missing-target-person":
					candidate.person = nil
				case "changed-native-pixel-version":
					owned := *candidate.nativeReference
					owned.native.protection.selection.registered.objects[1].object.VersionID += "-foreign"
					candidate.nativeReference = &owned
				}
				tx, err := scheduler.Begin(qualificationCtx)
				if err != nil {
					t.Fatal(err)
				}
				lockErr := LockPreparedSourcesInTransaction(qualificationCtx, tx, who, request, candidate)
				if lockErr == nil {
					lockErr = LockProtectionInTransaction(qualificationCtx, tx, who, request, candidate)
				}
				if lockErr == nil {
					lockErr = LockPersonProducerInTransaction(qualificationCtx, tx, who, request, candidate)
				}
				rollbackErr := tx.Rollback(qualificationCtx)
				if rollbackErr != nil || (mode == "original" && lockErr != nil) || (mode != "original" && lockErr == nil) {
					t.Fatal("native plan composed source/protection/person gates", mode, lockErr, rollbackErr)
				}
			}
			t.Log("V27_NATIVE_DEFECT_TRANSFER_PLAN_TARGET_GATES_OK")

			created, err := BeginDraft(qualificationCtx, scheduler, who, request, "proposal-publication", published, nativePlan, 1<<28)
			if err != nil || created.Outcome != "created" || created.RunID != request.RunID {
				t.Fatal("native transfer draft creation", created, err)
			}
			resumedPool, err := pgxpool.NewWithConfig(qualificationCtx, schedulerConfig.Copy())
			if err != nil {
				t.Fatal("native resume pool", err)
			}
			defer resumedPool.Close()
			resumed, err := BeginDraft(qualificationCtx, resumedPool, who, request, "proposal-publication", published, nativePlan, 1<<28)
			if err != nil || resumed.Outcome != "pending" || resumed.RunID != request.RunID {
				t.Fatal("native durable draft replay", resumed, err)
			}
			transport := v2input.ScopedPublisher{Documents: artifactjson.Publisher{Repository: artifactjson.SQLRepository{DB: scheduler, Bucket: fixture.Objects.Bucket}, Objects: objects}, Tenant: fixture.Tenant, Run: request.RunID}
			prepared, err := PublishWorkflowTasks(qualificationCtx, nativePlan, transport)
			if err != nil || len(prepared) != 1 || prepared[0].Graph.Built.Count < 1 {
				t.Fatal("native transport publication", err)
			}
			built := []v2input.Built{prepared[0].Graph.Built}

			observerCfg := cfg.Copy()
			delete(observerCfg.ConnConfig.RuntimeParams, "role")
			observer, err := pgxpool.NewWithConfig(qualificationCtx, observerCfg)
			if err != nil {
				t.Fatal("native registration observer", err)
			}
			defer observer.Close()
			expectedRows := map[int]v2input.Entry{}
			cursor, err := prepared[0].Graph.Open(qualificationCtx)
			if err != nil {
				t.Fatal(err)
			}
			for ordinal := 0; ; ordinal++ {
				entry, ok, err := cursor.Next(qualificationCtx)
				if err != nil {
					t.Fatal(err)
				}
				if !ok {
					break
				}
				expectedRows[ordinal] = entry
			}
			if closer, ok := cursor.(io.Closer); ok {
				if err := closer.Close(); err != nil {
					t.Fatal(err)
				}
			}
			if len(expectedRows) != built[0].Count {
				t.Fatal("native expected input count")
			}
			for i, ref := range built[0].Chunks {
				expectedRows[1000000+i] = v2input.Entry{Ref: ref, Contract: "InputClosureChunk"}
			}
			expectedRows[1000000+len(built[0].Chunks)] = v2input.Entry{Ref: built[0].Manifest, Contract: "InputClosureManifest"}
			assertStoredInputs := func(mode string, registered bool) {
				rows, err := observer.Query(qualificationCtx, "SELECT ordinal,artifact_id,sha256,contract,project_id FROM photo.v2_task_inputs WHERE tenant_id=$1 AND task_id=$2 ORDER BY ordinal", fixture.Tenant, nativePlan.tasks[0].ID)
				if err != nil {
					t.Fatal("native persisted input query", mode, err)
				}
				defer rows.Close()
				count := 0
				for rows.Next() {
					var ordinal int
					var entry v2input.Entry
					var project uuid.UUID
					if err := rows.Scan(&ordinal, &entry.Ref.ArtifactID, &entry.Ref.SHA256, &entry.Contract, &project); err != nil {
						t.Fatal(err)
					}
					expected, ok := expectedRows[ordinal]
					if !registered || !ok || entry != expected || project != fixture.Project {
						t.Fatal("native persisted input mismatch", mode, ordinal)
					}
					count++
				}
				if err := rows.Err(); err != nil {
					t.Fatal(err)
				}
				expectedCount := 0
				if registered {
					expectedCount = len(expectedRows)
				}
				if count != expectedCount {
					t.Fatal("native persisted input count", mode, count, expectedCount)
				}
				var hasManifest bool
				if err := observer.QueryRow(qualificationCtx, "SELECT execution_profile ? 'v2_input_manifest' FROM photo.tasks WHERE tenant_id=$1 AND id=$2", fixture.Tenant, nativePlan.tasks[0].ID).Scan(&hasManifest); err != nil || hasManifest != registered {
					t.Fatal("native persisted closure flag", mode, hasManifest, err)
				}
			}
			assertStoredInputs("before-registration", false)

			for _, mode := range []string{"rollback", "commit", "replay", "changed-manifest"} {
				pool := scheduler
				if mode == "replay" {
					pool = resumedPool
				}
				candidate := append([]v2input.Built(nil), built...)
				if mode == "changed-manifest" {
					candidate[0].Manifest.ArtifactID = uuid.New()
				}
				tx, err := pool.Begin(qualificationCtx)
				if err != nil {
					t.Fatal(err)
				}
				registrationErr := RegisterWorkflowInputsInTransaction(qualificationCtx, tx, who, request, published, nativePlan, candidate)
				if mode == "changed-manifest" {
					rollbackErr := tx.Rollback(qualificationCtx)
					if registrationErr != v2input.ErrConflict || rollbackErr != nil {
						t.Fatal("native altered manifest accepted", registrationErr, rollbackErr)
					}
				} else if registrationErr != nil {
					_ = tx.Rollback(qualificationCtx)
					t.Fatal("native input registration", mode, registrationErr)
				} else if mode == "rollback" {
					if err := tx.Rollback(qualificationCtx); err != nil {
						t.Fatal(err)
					}
				} else if err := tx.Commit(qualificationCtx); err != nil {
					t.Fatal("native registration commit", mode, err)
				}
				assertStoredInputs(mode, mode != "rollback")
			}
			t.Log("V27_NATIVE_DEFECT_TRANSFER_STORED_INPUTS_OK")
			t.Log("V27_NATIVE_DEFECT_TRANSFER_REGISTRATION_REPLAY_OK")

			preparer := TransferDraftPreparer{Reader: reader, Documents: publisher, Scheduler: resumedPool, TransportDocuments: transport.Documents, Configuration: configuration, ReservedBytes: 1 << 28}
			recovered, err := preparer.prepareNativeDraft(qualificationCtx, who, fixture.Tenant, "proposal-publication", request.Body, documents)
			if err != nil || recovered.RunID != request.RunID || recovered.WorkflowID != request.WorkflowID || recovered.Outcome != "pending" || len(recovered.transports) != 1 || recovered.transports[0].Manifest != built[0].Manifest || !recovered.plan.nativeReferenceRequired {
				t.Fatal("native integrated preparer recovery", err)
			}
			assertStoredInputs("native-preparer-recovery", true)
			t.Log("V27_NATIVE_DEFECT_TRANSFER_PREPARER_RECOVERY_OK")

			var operationEnabled bool
			if err := observer.QueryRow(qualificationCtx, "SELECT v2_enabled FROM photo.operation_registry WHERE operation='semantic.transfer' AND version='1'").Scan(&operationEnabled); err != nil || operationEnabled {
				t.Fatal("expected disabled transfer registry", operationEnabled, err)
			}
			disabledTx, err := resumedPool.Begin(qualificationCtx)
			if err != nil {
				t.Fatal(err)
			}
			deniedTicket, disabledErr := sealNativeTransferDraftInTransaction(qualificationCtx, disabledTx, who, request, "proposal-publication", published, recovered.plan, recovered.transports)
			disabledRollbackErr := disabledTx.Rollback(qualificationCtx)
			if disabledErr != v2input.ErrConflict || disabledRollbackErr != nil || deniedTicket.RunID != uuid.Nil {
				t.Fatal("disabled transfer admitted", disabledErr, disabledRollbackErr)
			}
			// Disposable owner fixture only; production operation remains disabled.
			tag, err := observer.Exec(qualificationCtx, "UPDATE photo.operation_registry SET v2_enabled=true WHERE operation='semantic.transfer' AND version='1' AND NOT v2_enabled")
			if err != nil || tag.RowsAffected() != 1 {
				t.Fatal("temporary transfer fixture capability", err)
			}
			defer func() {
				restoreCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				if _, err := observer.Exec(restoreCtx, "UPDATE photo.operation_registry SET v2_enabled=false WHERE operation='semantic.transfer' AND version='1'"); err != nil {
					t.Error("restore transfer capability", err)
				}
			}()

			activationTx, err := resumedPool.Begin(qualificationCtx)
			if err != nil {
				t.Fatal(err)
			}
			ticket, activationErr := sealNativeTransferDraftInTransaction(qualificationCtx, activationTx, who, request, "proposal-publication", published, recovered.plan, recovered.transports)
			rollbackErr := activationTx.Rollback(qualificationCtx)
			if activationErr != nil || rollbackErr != nil || ticket.RunID != request.RunID {
				t.Fatal("native activation rollback", activationErr, rollbackErr)
			}
			var status string
			var sealed bool
			var events int
			if err := observer.QueryRow(qualificationCtx, "SELECT status,graph_sealed FROM photo.runs WHERE tenant_id=$1 AND id=$2", fixture.Tenant, request.RunID).Scan(&status, &sealed); err != nil || status != "draft" || sealed {
				t.Fatal("native activation rollback leaked run", status, sealed, err)
			}
			if err := observer.QueryRow(qualificationCtx, "SELECT count(*) FROM photo.run_events WHERE tenant_id=$1 AND run_id=$2 AND type='v2.run.queued'", fixture.Tenant, request.RunID).Scan(&events); err != nil || events != 0 {
				t.Fatal("native activation rollback leaked events", events, err)
			}
			assertStoredInputs("activation-rollback", true)
			t.Log("V27_NATIVE_DEFECT_TRANSFER_ACTIVATION_ROLLBACK_OK")

			committedTicket, err := activateNativeTransfer(qualificationCtx, resumedPool, who, request, "proposal-publication", published, recovered)
			if err != nil {
				t.Fatal("native activation wrapper initial commit", err)
			}

			if committedTicket != ticket {
				t.Fatal("native committed ticket changed")
			}
			var taskState string
			if err := observer.QueryRow(qualificationCtx, "SELECT r.status,r.graph_sealed,t.state FROM photo.runs r JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id WHERE r.tenant_id=$1 AND r.id=$2 AND t.id=$3", fixture.Tenant, request.RunID, recovered.plan.tasks[0].ID).Scan(&status, &sealed, &taskState); err != nil || status != "queued" || !sealed || taskState != "ready" {
				t.Fatal("native durable activation state", status, sealed, taskState, err)
			}
			var outbox int
			if err := observer.QueryRow(qualificationCtx, "SELECT (SELECT count(*) FROM photo.run_events WHERE tenant_id=$1 AND run_id=$2 AND type='v2.run.queued'),(SELECT count(*) FROM photo.outbox WHERE tenant_id=$1 AND routing_key='v2.run.queued' AND payload->>'run_id'=$2::text)", fixture.Tenant, request.RunID).Scan(&events, &outbox); err != nil || events != 1 || outbox != 1 {
				t.Fatal("native queued event/outbox count", events, outbox, err)
			}
			assertStoredInputs("activation-commit", true)
			t.Log("V27_NATIVE_DEFECT_TRANSFER_ACTIVATION_COMMIT_OK")

			completedPool, err := pgxpool.NewWithConfig(qualificationCtx, schedulerConfig.Copy())
			if err != nil {
				t.Fatal(err)
			}
			defer completedPool.Close()
			for _, mode := range []string{"original", "changed-manifest", "foreign-session", "original-again"} {
				principal := who
				candidate := append([]v2input.Built(nil), recovered.transports...)
				if mode == "changed-manifest" {
					candidate[0].Manifest.ArtifactID = uuid.New()
				}
				if mode == "foreign-session" {
					principal.SessionID = uuid.New()
				}
				replayTx, err := completedPool.Begin(qualificationCtx)
				if err != nil {
					t.Fatal(err)
				}
				replayTicket, replayErr := replayNativeTransferInTransaction(qualificationCtx, replayTx, principal, request, "proposal-publication", published, recovered.plan, candidate)
				if mode == "original" || mode == "original-again" {
					if replayErr != nil || replayTicket != committedTicket {
						_ = replayTx.Rollback(qualificationCtx)
						t.Fatal("native completed replay", mode, replayErr)
					}
					if err := replayTx.Commit(qualificationCtx); err != nil {
						t.Fatal(err)
					}
				} else {
					rollbackErr := replayTx.Rollback(qualificationCtx)
					if replayErr == nil || replayTicket.RunID != uuid.Nil || rollbackErr != nil {
						t.Fatal("native completed replay accepted mutation", mode, replayErr, rollbackErr)
					}
				}
				if err := observer.QueryRow(qualificationCtx, "SELECT (SELECT count(*) FROM photo.run_events WHERE tenant_id=$1 AND run_id=$2 AND type='v2.run.queued'),(SELECT count(*) FROM photo.outbox WHERE tenant_id=$1 AND routing_key='v2.run.queued' AND payload->>'run_id'=$2::text)", fixture.Tenant, request.RunID).Scan(&events, &outbox); err != nil || events != 1 || outbox != 1 {
					t.Fatal("native replay duplicated event/outbox", mode, events, outbox, err)
				}
				assertStoredInputs("completed-replay-"+mode, true)
			}
			t.Log("V27_NATIVE_DEFECT_TRANSFER_COMPLETED_REPLAY_OK")

			for _, mode := range []string{"original", "changed-manifest", "foreign-session", "original-again"} {
				principal := who
				candidate := recovered
				candidate.transports = append([]v2input.Built(nil), recovered.transports...)
				if mode == "changed-manifest" {
					candidate.transports[0].Manifest.ArtifactID = uuid.New()
				}
				if mode == "foreign-session" {
					principal.SessionID = uuid.New()
				}
				recoveredTicket, recoveryErr := activateNativeTransfer(qualificationCtx, completedPool, principal, request, "proposal-publication", published, candidate)
				if mode == "original" || mode == "original-again" {
					if recoveryErr != nil || recoveredTicket != committedTicket {
						t.Fatal("native activation wrapper recovery", mode, recoveryErr)
					}
				} else if recoveryErr == nil || recoveredTicket.RunID != uuid.Nil {
					t.Fatal("native activation wrapper accepted mutation", mode, recoveryErr)
				}
				if err := observer.QueryRow(qualificationCtx, "SELECT (SELECT count(*) FROM photo.run_events WHERE tenant_id=$1 AND run_id=$2 AND type='v2.run.queued'),(SELECT count(*) FROM photo.outbox WHERE tenant_id=$1 AND routing_key='v2.run.queued' AND payload->>'run_id'=$2::text)", fixture.Tenant, request.RunID).Scan(&events, &outbox); err != nil || events != 1 || outbox != 1 {
					t.Fatal("native activation wrapper duplicated event/outbox", mode, events, outbox, err)
				}
				assertStoredInputs("activation-wrapper-"+mode, true)
			}
			t.Log("V27_NATIVE_DEFECT_TRANSFER_ACTIVATION_WRAPPER_OK")

			originalNative := nativeTransferPreparation{Intent: fixture.NativeIntent, Snapshot: fixture.NativeSnapshot, Protection: root, Task: task, Reports: reports}
			reconstructedTicket, err := preparer.prepareAndActivateNativeTransfer(qualificationCtx, who, fixture.Tenant, "proposal-publication", request.Body, originalNative)
			if err != nil || reconstructedTicket != committedTicket {
				t.Fatal("native reconstructed coordinator replay", err)
			}
			assertStoredInputs("reconstructed-coordinator", true)
			t.Log("V27_NATIVE_DEFECT_TRANSFER_COORDINATOR_RECOVERY_OK")

			workflows := v2workflow.Service{DB: db}
			beforeCancel, err := workflows.Get(qualificationCtx, who, fixture.Tenant, request.WorkflowID)
			if err != nil || beforeCancel.State != "queued" {
				t.Fatal("native workflow before cancellation", err)
			}
			cancelRequest := v2workflow.CancelRequest{ExpectedRevision: beforeCancel.Revision}
			firstCancel, err := workflows.Cancel(qualificationCtx, who, fixture.Tenant, request.WorkflowID, "native-transfer-cancellation", cancelRequest)
			if err != nil || firstCancel.Status != 200 {
				t.Fatal("native workflow cancellation", err)
			}
			repeatedCancel, err := workflows.Cancel(qualificationCtx, who, fixture.Tenant, request.WorkflowID, "native-transfer-cancellation", cancelRequest)
			if err != nil || string(repeatedCancel.Body) != string(firstCancel.Body) {
				t.Fatal("native cancellation replay", err)
			}
			var cancelling v2workflow.Status
			if json.Unmarshal(firstCancel.Body, &cancelling) != nil || cancelling.State != "cancelling" {
				t.Fatal("native cancellation status")
			}
			cancellationTx, err := completedPool.Begin(qualificationCtx)
			if err != nil {
				t.Fatal(err)
			}
			var cancelledTasks int
			if err := cancellationTx.QueryRow(qualificationCtx, "SELECT photo.cancel_idle_task()").Scan(&cancelledTasks); err != nil || cancelledTasks < 1 {
				_ = cancellationTx.Rollback(qualificationCtx)
				t.Fatal("native idle task cancellation", cancelledTasks, err)
			}
			var finalized string
			if err := cancellationTx.QueryRow(qualificationCtx, "SELECT photo.finalize_v2_run($1,$2)", fixture.Tenant, request.RunID).Scan(&finalized); err != nil || finalized != "cancelled" {
				_ = cancellationTx.Rollback(qualificationCtx)
				t.Fatal("native cancellation finalization", finalized, err)
			}
			if err := cancellationTx.Commit(qualificationCtx); err != nil {
				t.Fatal(err)
			}
			afterCancel, err := workflows.Get(qualificationCtx, who, fixture.Tenant, request.WorkflowID)
			if err != nil || afterCancel.State != "cancelled" {
				t.Fatal("native cancelled workflow visibility", err)
			}
			historicalTicket, err := preparer.prepareAndActivateNativeTransfer(qualificationCtx, who, fixture.Tenant, "proposal-publication", request.Body, originalNative)
			if err != nil || historicalTicket != committedTicket {
				t.Fatal("native historical admission after cancellation", err)
			}
			var cancelRequested bool
			var quotaBytes int64
			var quotaState string
			if err := observer.QueryRow(qualificationCtx, "SELECT r.status,r.cancel_requested,t.state,q.state,q.bytes FROM photo.runs r JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id JOIN photo.quota_reservations q ON q.tenant_id=r.tenant_id AND q.holder_type='run' AND q.holder_id=r.id WHERE r.tenant_id=$1 AND r.id=$2 AND t.id=$3", fixture.Tenant, request.RunID, recovered.plan.tasks[0].ID).Scan(&status, &cancelRequested, &taskState, &quotaState, &quotaBytes); err != nil || status != "cancelled" || !cancelRequested || taskState != "cancelled" || quotaState != "released" || quotaBytes != 0 {
				t.Fatal("native replay revived cancellation/quota", status, taskState, quotaState, quotaBytes, err)
			}
			if err := observer.QueryRow(qualificationCtx, "SELECT (SELECT count(*) FROM photo.run_events WHERE tenant_id=$1 AND run_id=$2 AND type='v2.run.queued'),(SELECT count(*) FROM photo.outbox WHERE tenant_id=$1 AND routing_key='v2.run.queued' AND payload->>'run_id'=$2::text)", fixture.Tenant, request.RunID).Scan(&events, &outbox); err != nil || events != 1 || outbox != 1 {
				t.Fatal("native cancellation replay duplicated activation", events, outbox, err)
			}
			assertStoredInputs("cancelled-historical-replay", true)
			t.Log("V27_NATIVE_DEFECT_TRANSFER_CANCELLATION_REPLAY_OK")

			var schedulerLookup, apiLookup bool
			if err := observer.QueryRow(qualificationCtx, "SELECT has_function_privilege('photo_scheduler','photo.read_v27_native_ready_transport(uuid,uuid,uuid,text,uuid,uuid,text,bigint,text)','EXECUTE'),has_function_privilege('photo_api','photo.read_v27_native_ready_transport(uuid,uuid,uuid,text,uuid,uuid,text,bigint,text)','EXECUTE')").Scan(&schedulerLookup, &apiLookup); err != nil || !schedulerLookup || apiLookup {
				t.Fatal("ready transport function ACL", schedulerLookup, apiLookup, err)
			}
			var manifestBytes int64
			if err := observer.QueryRow(qualificationCtx, "SELECT bytes FROM photo.artifacts WHERE tenant_id=$1 AND id=$2", fixture.Tenant, recovered.transports[0].Manifest.ArtifactID).Scan(&manifestBytes); err != nil {
				t.Fatal(err)
			}
			for _, mode := range []string{"original", "foreign-tenant", "foreign-actor", "foreign-session", "foreign-task", "wrong-purpose", "wrong-sha", "wrong-size", "wrong-bucket", "original-again"} {
				tenantID, actorID, sessionID, taskID := fixture.Tenant, who.UserID, who.SessionID, recovered.plan.tasks[0].ID
				purpose, sha, bucket := "v2_input_manifest", recovered.transports[0].Manifest.SHA256, fixture.Objects.Bucket
				size := manifestBytes
				switch mode {
				case "foreign-tenant":
					tenantID = uuid.New()
				case "foreign-actor":
					actorID = uuid.New()
				case "foreign-session":
					sessionID = uuid.New()
				case "foreign-task":
					taskID = uuid.New()
				case "wrong-purpose":
					purpose = "task_params"
				case "wrong-sha":
					sha = strings.Repeat("0", 64)
				case "wrong-size":
					size++
				case "wrong-bucket":
					bucket += "-foreign"
				}
				var matches int
				if err := completedPool.QueryRow(qualificationCtx, "SELECT count(*) FROM photo.read_v27_native_ready_transport($1,$2,$3,$4,$5,$6,$7,$8,$9)", tenantID, request.RunID, taskID, purpose, actorID, sessionID, sha, size, bucket).Scan(&matches); err != nil {
					t.Fatal("ready transport lookup", mode, err)
				}
				expected := 0
				if mode == "original" || mode == "original-again" {
					expected = 1
				}
				if matches != expected {
					t.Fatal("ready transport scope leaked", mode, matches, expected)
				}
			}
			assertStoredInputs("ready-transport-denials", true)
			t.Log("V27_NATIVE_DEFECT_TRANSFER_READY_TRANSPORT_SCOPE_OK")

			t.Log("V27_NATIVE_DEFECT_REFERENCE_CONTINUATION_TRANSACTION_LOCK_OK")
		}
		substituted := reports
		substituted.Defect = reports.Parser[0]
		denied, err := reader.prepareQualifiedNativeDefectResult(qualificationCtx, who, request, "proposal-publication", fixture.NativeIntent, fixture.NativeSnapshot, task, substituted, color)
		if err == nil || denied.verified || len(denied.faces) != 0 || len(denied.parsers) != 0 {
			t.Fatal("substituted defects qualification accepted", err)
		}
		t.Log("V27_NATIVE_DEFECT_QUALIFIED_PRODUCER_TRANSACTION_LOCK_OK")
	})
	t.Log("V27_NATIVE_DEFECT_WORKER_CONTENT_OK")
}
