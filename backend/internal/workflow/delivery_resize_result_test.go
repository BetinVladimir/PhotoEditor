package workflow

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/exportpreset"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestDeliveryResizeResultGeometry(t *testing.T) {
	plan, parent, child, raw, _ := deliveryExportPlanFixture(t, func(c *exportpreset.Config) {
		edge := 200
		c.Size = &exportpreset.Size{LongEdge: &edge, Fit: "contain"}
		c.Sharpen = &exportpreset.Sharpen{Radius: 1.2, Strength: .4, Threshold: .02}
	})
	registry, err := LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	node, err := CompileDeliveryPresentation(plan, parent, child, raw, registry)
	if err != nil {
		t.Fatal(err)
	}
	image, err := review.ParseWorkingImage(raw)
	if err != nil {
		t.Fatal(err)
	}
	canvas, chain, err := deliveryResizeGeometry(*node, image)
	if err != nil {
		t.Fatal(err)
	}
	if canvas.Width != 200 || canvas.Height != 100 || canvas.SourceSHA256 != image.Canvas.SourceSHA256 {
		t.Fatal("wrong resize dimensions")
	}
	image.Canvas = canvas
	image.TransformChain = chain
	image.Pixels = review.ArtifactRef{ID: uuid.New(), SHA256: downloadDigest("resized pixels")}
	outputRaw, _ := canonical.Marshal(image)
	outputRef := review.ArtifactRef{ID: uuid.New(), SHA256: renderDerivativeBytesSHA(outputRaw)}
	reportRaw, _ := canonical.Marshal(DeliveryDerivativeReport{Image: node.Inputs[0], Operation: "resize", Applied: true, Width: 200, Height: 100, Warnings: []string{}})
	reportRef := review.ArtifactRef{ID: uuid.New(), SHA256: renderDerivativeBytesSHA(reportRaw)}
	if _, err := ValidateDeliveryResizeResult(*node, raw, outputRef, outputRaw, reportRef, reportRaw); err != nil {
		t.Fatal(err)
	}
	next, err := CompileDeliveryAfterResize(plan, parent, child, raw, outputRef, outputRaw, reportRef, reportRaw, registry)
	if err != nil || next == nil || next.Operation != "derivative.sharpen" || next.Inputs[0] != outputRef || next.Inputs[1] != image.Pixels {
		t.Fatal("resize result not used for sharpen", err)
	}
	if _, err := CompileDeliveryDerivativeProfile(*next, outputRaw, DeliveryDerivativeLimits{RAMBytes: 4 << 30, ScratchBytes: 2 << 30, OutputBytes: 1 << 30}); err != nil {
		t.Fatal(err)
	}
	if _, err := CompileDeliveryAfterResize(plan, parent, child, raw, outputRef, append(outputRaw, ' '), reportRef, reportRaw, registry); err == nil {
		t.Fatal("unverified output used")
	}
	sharpenImage := image
	sharpenImage.Pixels = review.ArtifactRef{ID: uuid.New(), SHA256: downloadDigest("sharpened resized pixels")}
	sharpenRaw, _ := canonical.Marshal(sharpenImage)
	sharpenRef := review.ArtifactRef{ID: uuid.New(), SHA256: renderDerivativeBytesSHA(sharpenRaw)}
	sharpenReport, _ := canonical.Marshal(DeliveryDerivativeReport{Image: outputRef, Operation: "sharpen", Applied: true, Width: 200, Height: 100, MaxChannelDelta: .02, Warnings: []string{}})
	sharpenReportRef := review.ArtifactRef{ID: uuid.New(), SHA256: renderDerivativeBytesSHA(sharpenReport)}
	results := []DeliveryPresentationOutput{{ImageRef: outputRef, ImageJSON: outputRaw, ReportRef: reportRef, ReportJSON: reportRaw}, {ImageRef: sharpenRef, ImageJSON: sharpenRaw, ReportRef: sharpenReportRef, ReportJSON: sharpenReport}}
	limits := DeliveryDerivativeLimits{RAMBytes: 4 << 30, ScratchBytes: 2 << 30, OutputBytes: 1 << 30}
	workflow, _ := v2input.WorkflowID(child.RunID)
	for count, operation := range []string{"derivative.resize", "derivative.sharpen", "export.encode"} {
		continuation, err := CompileDeliveryContinuation(plan, parent, child, raw, results[:count], downloadDigest("target icc"), registry, limits)
		expectedID, _ := v2input.TaskID(workflow, operation, 0)
		if err != nil || continuation.Node.Operation != operation || continuation.Node.ID != expectedID || continuation.Stage != count {
			t.Fatal("continuation", count, err)
		}
		if count == 0 && continuation.Predecessor != uuid.Nil {
			t.Fatal("initial predecessor")
		}
		if count > 0 && continuation.Predecessor == uuid.Nil {
			t.Fatal("missing predecessor")
		}
		if _, err := ParseResourceEstimate(continuation.ExecutionProfile); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := CompileDeliveryContinuation(plan, parent, child, raw, []DeliveryPresentationOutput{results[1], results[0]}, downloadDigest("target icc"), registry, limits); err == nil {
		t.Fatal("reordered continuation accepted")
	}
	for count := 1; count <= 2; count++ {
		continuation, err := CompileDeliveryContinuation(plan, parent, child, raw, results[:count], downloadDigest("target icc"), registry, limits)
		if err != nil {
			t.Fatal(err)
		}
		resolved := []v2input.Entry{{Ref: child.Binding.Source, Contract: "WorkingImage"}, {Ref: child.Recipe, Contract: "EditingRecipeV3"}, {Ref: child.Preset.Config, Contract: "ExportPresetConfig"}}
		for _, result := range results[:count] {
			currentImage, err := review.ParseWorkingImage(result.ImageJSON)
			if err != nil {
				t.Fatal(err)
			}
			resolved = append(resolved, v2input.Entry{Ref: v2input.Ref{ArtifactID: result.ImageRef.ID, SHA256: result.ImageRef.SHA256}, Contract: "WorkingImage"}, v2input.Entry{Ref: v2input.Ref{ArtifactID: result.ReportRef.ID, SHA256: result.ReportRef.SHA256}, Contract: "DerivativeReport"}, v2input.Entry{Ref: v2input.Ref{ArtifactID: currentImage.Pixels.ID, SHA256: currentImage.Pixels.SHA256}, Contract: "NpyArray"})
			if currentImage.Alpha != nil {
				resolved = append(resolved, v2input.Entry{Ref: v2input.Ref{ArtifactID: currentImage.Alpha.ID, SHA256: currentImage.Alpha.SHA256}, Contract: "NpyArray"})
			}
		}
		savedRef := v2input.Ref{ArtifactID: uuid.New(), SHA256: renderDerivativeBytesSHA(continuation.Node.Params)}
		savedRaw, _ := canonical.Marshal(savedRef)
		savedDB := &deliveryStageReadDB{responses: [][]byte{savedRaw}}
		recoveredParams, err := ReadSavedDeliveryContinuationParams(context.Background(), savedDB, delivery.ExpansionLease{Run: parent}, child, continuation.Node)
		if err != nil || recoveredParams == nil || *recoveredParams != savedRef {
			t.Fatal("saved continuation Params", err)
		}
		changedRef := savedRef
		changedRef.SHA256 = downloadDigest("changed params")
		changedRaw, _ := canonical.Marshal(changedRef)
		if _, err := ReadSavedDeliveryContinuationParams(context.Background(), &deliveryStageReadDB{responses: [][]byte{changedRaw}}, delivery.ExpansionLease{Run: parent}, child, continuation.Node); err == nil {
			t.Fatal("foreign saved Params accepted")
		}
		paramsDB := &deliveryParamsDB{row: deliveryParamsRow{outcome: "created", id: uuid.New(), token: uuid.New()}}
		paramsLease := delivery.ExpansionLease{Expansion: uuid.New(), Token: uuid.New(), Tenant: uuid.New(), Run: parent, SlotCount: 1, Cursor: 0}
		repo := deliveryParamsRepository{db: paramsDB, bucket: "documents", lease: paramsLease, slot: child.SlotIndex, owner: continuation.Node.ID, params: append([]byte(nil), continuation.Node.Params...)}
		if _, err := repo.Reserve(context.Background(), repo.scope(), renderDerivativeBytesSHA(repo.params), int64(len(repo.params))); err != nil {
			t.Fatal("continuation Params reserve adapter", err)
		}
		if paramsDB.args[3] != string(continuation.Node.Params) || paramsDB.args[7] != continuation.Node.ID || paramsDB.args[6] != parent {
			t.Fatal("continuation Params scope drift")
		}
		params := v2input.Ref{ArtifactID: uuid.New(), SHA256: renderDerivativeBytesSHA(continuation.Node.Params)}
		frozen, err := FreezeDeliveryContinuationTask(plan, parent, child, raw, results[:count], downloadDigest("target icc"), registry, limits, params, resolved)
		if err != nil {
			t.Fatal("freeze continuation", count, err)
		}
		for _, accepted := range []bool{true, false} {
			tx := &deliveryParamsDB{row: deliveryParamsRow{accepted: accepted}}
			registerLease := paramsLease
			registerLease.ParentTask = plan.TaskID()
			registerErr := frozen.CreateInTransaction(context.Background(), tx, registerLease, plan, child)
			if (registerErr == nil) != accepted || tx.calls != 1 || tx.args[3] != count || tx.args[4] != continuation.Predecessor {
				t.Fatal("continuation registration", registerErr)
			}
		}
		graph, err := frozen.PublishInputs(context.Background(), deliveryTransportPublisher{})
		if err != nil {
			t.Fatal(err)
		}
		graph.Open = func(context.Context) (v2input.Cursor, error) { panic("caller-replaced continuation cursor used") }
		graph.Direct = nil
		registerLease := paramsLease
		registerLease.ParentTask = plan.TaskID()
		for _, accepted := range []bool{true, false} {
			activationTX := &deliveryActivationTx{accepted: accepted}
			activationErr := frozen.ActivateInTransaction(context.Background(), activationTX, registerLease, plan, child)
			if (activationErr == nil) != accepted || len(activationTX.calls) != 2 || activationTX.args[3] != count || activationTX.args[4] != continuation.Predecessor {
				t.Fatal("dynamic activation binding", activationErr)
			}
		}
		activatedTX := &deliveryActivationTx{activatedReady: true}
		if err := frozen.ActivateInTransaction(context.Background(), activatedTX, registerLease, plan, child); err != nil || len(activatedTX.calls) != 1 {
			t.Fatal("activation replay mutated graph", err)
		}
		transportTX := &deliveryActivationTx{accepted: true}
		if err := frozen.RegisterInputsInTransaction(context.Background(), transportTX, registerLease, plan, child, graph); err != nil {
			t.Fatal("continuation transport registration", err)
		}
		if len(transportTX.calls) != 4 || !strings.Contains(transportTX.calls[0], "continuation_transport_ready") || !strings.Contains(transportTX.calls[1], "create_v30_delivery_continuation_task") || !strings.Contains(transportTX.calls[2], "register_v2_task_input_batch") || !strings.Contains(transportTX.calls[3], "seal_v2_task_inputs") {
			t.Fatal("transport registration order", transportTX.calls)
		}
		recoveredTX := &deliveryActivationTx{transportReady: true}
		if err := frozen.RegisterInputsInTransaction(context.Background(), recoveredTX, registerLease, plan, child, graph); err != nil || len(recoveredTX.calls) != 1 {
			t.Fatal("registered continuation replay duplicated inputs", err)
		}
		refusedTX := &deliveryActivationTx{accepted: false}
		if err := frozen.RegisterInputsInTransaction(context.Background(), refusedTX, registerLease, plan, child, graph); err == nil || len(refusedTX.calls) != 2 {
			t.Fatal("refused continuation registered inputs")
		}
		exerciseContinuationCoordinator(t, frozen)
		draft, err := frozen.DraftTask()
		if err != nil || draft.ID != continuation.Node.ID || frozen.Predecessor() != continuation.Predecessor || frozen.Stage() != count {
			t.Fatal("frozen descriptor", err)
		}
		draft.ExecutionProfile[0] = 'x'
		again, _ := frozen.DraftTask()
		if again.ExecutionProfile[0] != '{' {
			t.Fatal("borrowed continuation profile")
		}
		if _, err := FreezeDeliveryContinuationTask(plan, parent, child, raw, results[:count], downloadDigest("target icc"), registry, limits, params, resolved[:3]); err == nil {
			t.Fatal("missing adopted roots accepted")
		}
	}
	encoded, err := CompileDeliveryFinalEncode(plan, parent, child, raw, results, downloadDigest("target icc"), registry)
	if err != nil || encoded.Inputs[0] != sharpenRef || encoded.Inputs[1] != sharpenImage.Pixels {
		t.Fatal("final encoder did not use validated chain", err)
	}
	if _, err := CompileDeliveryEncodeProfile(encoded, sharpenRaw, DeliveryDerivativeLimits{RAMBytes: 4 << 30, ScratchBytes: 2 << 30, OutputBytes: 1 << 30}); err != nil {
		t.Fatal("final encode profile", err)
	}
	if _, err := CompileDeliveryEncodeProfile(encoded, raw, DeliveryDerivativeLimits{RAMBytes: 4 << 30, ScratchBytes: 2 << 30, OutputBytes: 1 << 30}); err == nil {
		t.Fatal("original source substituted for final image")
	}
	var encode encodeParams
	if json.Unmarshal(encoded.Params, &encode) != nil || encode.Size != nil {
		t.Fatal("resize applied twice")
	}
	if _, err := CompileDeliveryFinalEncode(plan, parent, child, raw, results[:1], downloadDigest("target icc"), registry); err == nil {
		t.Fatal("missing sharpen accepted")
	}
	if _, err := CompileDeliveryFinalEncode(plan, parent, child, raw, []DeliveryPresentationOutput{results[1], results[0]}, downloadDigest("target icc"), registry); err == nil {
		t.Fatal("reordered results accepted")
	}
	image.Color.ICCSHA256 = downloadDigest("other")
	badRaw, _ := canonical.Marshal(image)
	badRef := outputRef
	badRef.SHA256 = renderDerivativeBytesSHA(badRaw)
	if _, err := ValidateDeliveryResizeResult(*node, raw, badRef, badRaw, reportRef, reportRaw); err == nil {
		t.Fatal("color substitution accepted")
	}
	if _, err := ValidateDeliveryResizeResult(*node, raw, outputRef, outputRaw, reportRef, append(reportRaw, ' ')); err == nil {
		t.Fatal("report mismatch accepted")
	}
}
