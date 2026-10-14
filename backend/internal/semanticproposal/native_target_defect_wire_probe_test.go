package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func probeNativeTargetDefectParams(t *testing.T, ctx context.Context, p TransferDraftPreparer, who auth.Identity, original detectorDraftPlan, skin nativeTargetSkinDraftPlan, owner *pgxpool.Pool) {
	t.Helper()
	var draftPlan *nativeTargetDefectDraftPlan
	d := skin.parser.detector
	a := d.anchor
	catalog, err := (SQLNativeParentArtifactCatalogFactory{API: p.Reader.DB, Bucket: p.Reader.Bucket}).ForReference(ctx, who, a.request.TenantID, p.Reader.ProjectID, a.entry.Binding.AssetVersionID)
	if err != nil {
		t.Fatal(err)
	}
	anchor := func(op string) nativeTargetAnchor {
		t.Helper()
		out, err := compileNativeTargetAnchor(ctx, who, a.request, a.key, a.published, a.snapshot, a.snapshotBody, a.manifest, a.entry.Ordinal, op)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	la := anchor("face.landmarks")
	ls, err := p.Reader.readNativeTargetFaceSaved(ctx, who, la)
	if err != nil || ls.RunState != "completed" {
		t.Fatal("target defect landmarks saved", err)
	}
	landmarks, err := p.Reader.restoreNativeTargetFacePlan(ctx, catalog, who, d, la, p.Configuration.Color, ls)
	if err != nil {
		t.Fatal(err)
	}
	pa := anchor("mask.person")
	ps, err := p.Reader.readNativeTargetPersonSaved(ctx, who, pa)
	if err != nil || ps.RunState != "completed" {
		t.Fatal("target defect person saved", err)
	}
	person, err := p.Reader.restoreNativeTargetPersonPlan(ctx, catalog, who, pa, p.Configuration.Color, ps)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := p.Reader.prepareNativeTargetDefectParams(ctx, who, landmarks, skin, person, p.Configuration.Color, [2]float64{.002, .012})
	if err != nil {
		t.Fatal("target defect physical preparation", err)
	}
	for _, kind := range []string{"params", "diameter", "person", "landmarks", "skin", "namespace"} {
		changed := prepared
		switch kind {
		case "params":
			changed.params.params = "{}"
		case "diameter":
			changed.diameter = [2]float64{.003, .012}
		case "person":
			changed.person.planSeal = "changed"
		case "landmarks":
			changed.landmarks.body = "{}"
		case "skin":
			changed.skin.skinPixels++
		case "namespace":
			changed.personPlan.anchor.entry.Namespace = a.request.RunID
		}
		if verifyNativeTargetDefectPreparation(ctx, changed) == nil {
			t.Fatal("target defect substitution accepted", kind)
		}
	}
	published, err := p.Reader.publishNativeTargetDefectParams(ctx, who, prepared, p.Configuration.Color, p.Documents)
	if err != nil {
		t.Fatal("target defect Params publication", err)
	}
	again, err := p.Reader.publishNativeTargetDefectParams(ctx, who, prepared, p.Configuration.Color, p.Documents)
	if err != nil || again.Ref != published.Ref {
		t.Fatal("target defect Params replay", err)
	}
	document := anchor("defects.propose")
	_ = document // actual DAG anchor must exist; publication cannot create a task.
	binding := a.entry.Binding
	id := targetDefectParamsDocument(a.entry.Namespace)
	object, err := p.Reader.defectVersionParamsObject(ctx, who, a.request.TenantID, binding, id, published.Ref)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := v2input.ReadPinnedSemanticDocument(ctx, p.Reader.Objects, object, published.Ref)
	if err != nil || string(raw) != prepared.params.params {
		t.Fatal("target defect physical Params", err)
	}
	if _, err := owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1", who.SessionID); err != nil {
		t.Fatal(err)
	}
	_, revokedErr := p.Reader.publishNativeTargetDefectParams(ctx, who, prepared, p.Configuration.Color, p.Documents)
	if _, err := owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=NULL WHERE id=$1", who.SessionID); err != nil {
		t.Fatal(err)
	}
	if revokedErr == nil {
		t.Fatal("target defect revoked publication accepted")
	}
	if err := p.Reader.recheckNativeTargetDefectPreparation(ctx, who, prepared, p.Configuration.Color); err != nil {
		t.Fatal("target defect restored authority", err)
	}
	if os.Getenv("PHOTO_TEST_V27_TARGET_DEFECT_QUALIFICATION") == "1" {
		cfg, err := p.Reader.selectInitialDefectConfiguration(ctx, who, a.request.TenantID, p.Configuration, p.ReservedBytes)
		if err != nil {
			t.Fatal("target defect server qualification", err)
		}
		qualified, err := p.Reader.prepareNativeTargetDefectQualification(ctx, who, prepared, p.Configuration.Color, cfg.Report, cfg.Budget.Costs)
		if err != nil {
			t.Fatal("target defect qualification", err)
		}
		for _, kind := range []string{"preparation", "report", "cost", "gpu", "policy", "session", "object", "metrics"} {
			changed := qualified
			switch kind {
			case "preparation":
				changed.preparationSeal = "changed"
			case "report":
				changed.report.expected = "{}"
			case "cost":
				changed.costs.RAMBytes++
			case "gpu":
				changed.costs.VRAMBytes = 1
			case "policy":
				changed.policy.body = "[]"
			case "session":
				changed.report.session = a.request.RunID
			case "object":
				changed.report.report.object.VersionID = "changed"
			case "metrics":
				changed.report.metrics.PeakRSSBytes++
			}
			if p.Reader.verifyNativeTargetDefectQualification(ctx, who, prepared, changed) == nil {
				t.Fatal("target qualification substitution accepted", kind)
			}
		}
		if err := p.Reader.recheckNativeTargetDefectQualification(ctx, who, prepared, qualified, p.Configuration.Color); err != nil {
			t.Fatal("target qualification replay", err)
		}
		if _, err := owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1", who.SessionID); err != nil {
			t.Fatal(err)
		}
		revokedErr := p.Reader.recheckNativeTargetDefectQualification(ctx, who, prepared, qualified, p.Configuration.Color)
		if _, err := owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=NULL WHERE id=$1", who.SessionID); err != nil {
			t.Fatal(err)
		}
		if revokedErr == nil {
			t.Fatal("target qualification revoked session accepted")
		}
		if err := p.Reader.recheckNativeTargetDefectQualification(ctx, who, prepared, qualified, p.Configuration.Color); err != nil {
			t.Fatal("target qualification restored session", err)
		}

		if os.Getenv("PHOTO_TEST_V27_TARGET_DEFECT_TASK") == "1" {
			compiled, err := p.Reader.prepareNativeTargetDefectTask(ctx, who, prepared, qualified, p.Configuration.Color, published, cfg.Budget.RAM, cfg.Budget.Scratch, cfg.Budget.Output)
			if err != nil {
				t.Fatal("target defect physical task", err)
			}
			again, err := p.Reader.prepareNativeTargetDefectTask(ctx, who, prepared, qualified, p.Configuration.Color, published, cfg.Budget.RAM, cfg.Budget.Scratch, cfg.Budget.Output)
			if err != nil || again.seal != compiled.seal {
				t.Fatal("target defect deterministic task", err)
			}
			draft, err := compiled.frozen.DraftTask()
			expectedRun := uuid.NewSHA1(a.entry.Namespace, []byte("semantic.defect.prepare:reference"))
			workflow, workflowErr := v2input.WorkflowID(expectedRun)
			expectedTask, taskErr := v2input.TaskID(workflow, "defects.propose", 0)
			if err != nil || workflowErr != nil || taskErr != nil || draft.ID != expectedTask {
				t.Fatal("target defect task namespace", err)
			}
			transport, err := v2input.PublishSemanticDefectTask(ctx, compiled.frozen, &proposalMemoryTransport{})
			if err != nil || transport.Graph.ID != draft.ID {
				t.Fatal("target defect memory transport", err)
			}
			for _, kind := range []string{"params", "person", "size", "version", "media"} {
				params, pixels := compiled.params, compiled.personPixels
				switch kind {
				case "params":
					params.ref.SHA256 = "changed"
				case "person":
					pixels.ref.ArtifactID = uuid.New()
				case "size":
					pixels.object.Bytes = 0
				case "version":
					pixels.object.VersionID = "null"
				case "media":
					pixels.object.ContentType = "image/png"
				}
				if _, _, err := compileNativeTargetDefectInventory(ctx, prepared, params, pixels); err == nil {
					t.Fatal("target defect bad inventory accepted", kind)
				}
			}
			entries, sizes, err := compileNativeTargetDefectInventory(ctx, prepared, compiled.params, compiled.personPixels)
			if err != nil {
				t.Fatal(err)
			}
			entryBody, _ := canonical.Marshal(entries)
			sizeBody, _ := canonical.Marshal(sizes)
			if string(entryBody) != compiled.entries || string(sizeBody) != compiled.sizes {
				t.Fatal("target defect inventory replay")
			}
			if _, err := p.Reader.compileNativeTargetDefectTask(ctx, who, prepared, qualified, compiled.params, compiled.personPixels, 1, 1, 1); err == nil {
				t.Fatal("target defect insufficient capacity accepted")
			}
			if os.Getenv("PHOTO_TEST_V27_TARGET_DEFECT_PLAN") == "1" {
				value := probeNativeTargetDefectPlan(t, ctx, p, who, compiled, cfg.Budget.Reserved)
				draftPlan = &value
			}
			t.Log("V27_TARGET_DEFECT_PHYSICAL_TASK_INVENTORY_REPLAY_OK")
		}
		t.Log("V27_TARGET_DEFECT_QUALIFICATION_REPLAY_SUBSTITUTION_REVOKE_OK")
	}
	if os.Getenv("PHOTO_TEST_V27_TARGET_DEFECT_DEPENDENCIES") == "1" {
		probeNativeTargetDefectDependencies(t, ctx, p, who, original, prepared, draftPlan, owner)
	}
	t.Log("V27_TARGET_DEFECT_PHYSICAL_PARAMS_REPLAY_SUBSTITUTION_REVOKE_OK")
}
