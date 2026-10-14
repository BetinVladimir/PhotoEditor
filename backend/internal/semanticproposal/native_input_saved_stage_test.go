package semanticproposal

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// This fixture proves locator/profile decoding, not model or SQL qualification.
func TestNativeSavedStageOriginalLocatorAndScopeMutations(t *testing.T) {
	for _, operation := range []string{"face.detect", "face.landmarks", "face.parse", "mask.face_skin", "defects.propose", "mask.person"} {
		t.Run(operation, func(t *testing.T) {
			project := uuid.New()
			who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
			request := Request{RunID: uuid.New(), ActorID: who.UserID, TenantID: uuid.New(), RequestSHA: strings.Repeat("a", 64)}
			pin := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("b", 64)} }
			parent, err := v2input.RunID(request.TenantID, request.ActorID, "original-request")
			if err != nil {
				t.Fatal(err)
			}
			request.RunID = parent
			request.Intent.Reference.Source = pin()
			intent, snapshot, params := pin(), pin(), pin()
			run, task, marker, err := nativeInputSavedStageIdentity(request.RunID, operation)
			if err != nil {
				t.Fatal(err)
			}
			backend := "cuda"
			if operation == "mask.face_skin" || operation == "defects.propose" {
				backend = "cpu"
			}
			profile, err := canonical.Marshal(map[string]any{"backend": backend, "resource_estimate": map[string]int64{"ram_bytes": 1024, "vram_bytes": 0, "scratch_bytes": 2048, "output_bytes": 4096}})
			if err != nil {
				t.Fatal(err)
			}
			report := pin()
			var reportPtr *v2input.Ref = &report
			if operation == "mask.face_skin" {
				reportPtr = nil
			}
			costs := v2input.SemanticQualifiedCosts{}
			if operation == "mask.person" {
				costs.VRAMBytes = 1
				profile = json.RawMessage(`{"backend":"cuda","resource_estimate":{"ram_bytes":1024,"vram_bytes":1,"scratch_bytes":2048,"output_bytes":4096}}`)
			}
			metadata, err := nativePlanRecoveryFromTask(v2input.DraftTask{Operation: operation, ExecutionProfile: profile}, reportPtr, costs, 16<<20)
			if err != nil {
				t.Fatal(err)
			}
			if operation == "face.detect" || operation == "mask.person" {
				metadata.RequestKey = "original-request"
			}
			documentOperation := operation
			if operation == "defects.propose" {
				documentOperation = "defect"
			}
			inputDigest := strings.Repeat("c", 64)
			stage := map[string]any{"schema_version": 1, "parent_run_id": request.RunID, "request_sha": request.RequestSHA, "auth_session_id": who.SessionID, "source": request.Intent.Reference, "intent": intent, "snapshot": snapshot, "params_document": uuid.NewSHA1(request.RunID, []byte("semantic."+documentOperation+".params:reference")), "params": params, "task_id": task, "input_digest": inputDigest, "recovery": metadata}
			if operation != "face.detect" && operation != "mask.person" {
				stage["operation"] = operation
			}
			encode := func() ([]byte, string) {
				body, err := canonical.Marshal(map[string]any{"project_id": project, marker: stage})
				if err != nil {
					t.Fatal(err)
				}
				sha, err := canonical.Hash(json.RawMessage(body))
				if err != nil {
					t.Fatal(err)
				}
				return body, sha
			}
			body, sha := encode()
			got, err := decodeNativeInputSavedStage(body, sha, task, params, inputDigest, profile, project, who, request, intent, snapshot, operation)
			if err != nil || got.run != run || got.task != task || got.params != params || got.recovery.Reserved != 16<<20 {
				t.Fatal("original locator refused", err)
			}
			if got.person != nil {
				t.Fatal("legacy config acquired synthetic person input")
			}
			stage["person_input"] = nil
			nullPerson, nullSHA := encode()
			if denied, err := decodeNativeInputSavedStage(nullPerson, nullSHA, task, params, inputDigest, profile, project, who, request, intent, snapshot, operation); err == nil || denied.run != uuid.Nil {
				t.Fatal("explicit null person locator accepted")
			}
			delete(stage, "person_input")
			if operation == "face.detect" || operation == "mask.person" {
				changedMetadata := metadata
				changedMetadata.RequestKey = "other-request"
				stage["recovery"] = changedMetadata
				changed, changedSHA := encode()
				if denied, err := decodeNativeInputSavedStage(changed, changedSHA, task, params, inputDigest, profile, project, who, request, intent, snapshot, operation); err == nil || denied.run != uuid.Nil {
					t.Fatal("saved request key for a different parent accepted")
				}
				stage["recovery"] = metadata
			}
			for _, field := range []string{"parent_run_id", "request_sha", "auth_session_id", "source", "intent", "snapshot", "params_document", "params", "task_id", "input_digest", "recovery"} {
				original := stage[field]
				stage[field] = nil
				changed, changedSHA := encode()
				denied, err := decodeNativeInputSavedStage(changed, changedSHA, task, params, inputDigest, profile, project, who, request, intent, snapshot, operation)
				if err == nil || denied.run != uuid.Nil {
					t.Fatal("changed locator accepted", field)
				}
				stage[field] = original
			}
			if got, err := decodeNativeInputSavedStage(body, strings.Repeat("d", 64), task, params, inputDigest, profile, project, who, request, intent, snapshot, operation); err == nil || got.run != uuid.Nil {
				t.Fatal("changed saved config SHA accepted")
			}
		})
	}
}

func TestNativeSavedStageRecoveryCancelledBeforeIO(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reader := SQLTransferSourceReader{}
	if plan, err := reader.restorePersonDraftPlan(ctx, nil, auth.Identity{}, nativePreparationAnchor{}); err != context.Canceled || plan.seal != "" {
		t.Fatal("cancelled person recovery reached IO", err)
	}

	if producers, err := reader.restoreNativeInputFaceProducers(ctx, nil, auth.Identity{}, Request{}, "", PublishedIntent{}, v2input.Ref{}, nil, TransferColorProfile{}); !errors.Is(err, context.Canceled) || producers.detector.seal != "" {
		t.Fatal("cancelled producer reconstruction reached IO", err)
	}
	if request, key, err := reader.restoreNativeInputSavedRequest(ctx, auth.Identity{}, Request{}, v2input.Ref{}, v2input.Ref{}); !errors.Is(err, context.Canceled) || request.RunID != uuid.Nil || key != "" {
		t.Fatal("cancelled request identity recovery reached IO", err)
	}
	if saved, err := reader.readNativeInputSavedStage(ctx, auth.Identity{}, Request{}, v2input.Ref{}, v2input.Ref{}, "face.detect"); !errors.Is(err, context.Canceled) || saved.run != uuid.Nil {
		t.Fatal("cancelled locator reached IO", err)
	}
	if face, err := reader.restoreNativeFaceDraftPlan(ctx, nil, auth.Identity{}, detectorDraftPlan{}, TransferColorProfile{}, "face.landmarks"); !errors.Is(err, context.Canceled) || face.seal != "" {
		t.Fatal("cancelled face recovery reached IO", err)
	}
	if skin, err := reader.restoreNativeSkinDraftPlan(ctx, auth.Identity{}, nativeFaceDraftPlan{}, TransferColorProfile{}); !errors.Is(err, context.Canceled) || skin.seal != "" {
		t.Fatal("cancelled skin recovery reached IO", err)
	}
}
