package semanticproposal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

type detectorCatalogFixture struct {
	sizes  map[uuid.UUID]int64
	calls  map[uuid.UUID]int
	mode   string
	who    auth.Identity
	tenant uuid.UUID
}

func (c *detectorCatalogFixture) Lookup(_ context.Context, who auth.Identity, tenant uuid.UUID, node v2draft.LayerRef) (objectstore.Object, error) {
	if who.UserID != c.who.UserID || who.SessionID != c.who.SessionID || tenant != c.tenant {
		return objectstore.Object{}, v2input.ErrInvalid
	}
	if node.Contract == "NpyArray" {
		return objectstore.Object{}, v2input.ErrInvalid
	}
	if node.Contract == "WorkingPixels" || node.Contract == "WorkingAlpha" || node.Contract == "DenseWarpField" {
		if node.ParentRef.ArtifactID == uuid.Nil || node.ParentRef.SHA256 == "" {
			return objectstore.Object{}, v2input.ErrInvalid
		}
	}
	c.calls[node.Ref.ArtifactID]++
	n := c.calls[node.Ref.ArtifactID]
	if c.mode == "revoke" && n == 2 {
		return objectstore.Object{}, v2input.ErrInvalid
	}
	version := "v1"
	if c.mode == "mutate" && n == 2 {
		version = "v2"
	}
	size := c.sizes[node.Ref.ArtifactID]
	if c.mode == "size" && node.Contract == "Params" {
		size++
	}
	media := "application/json"
	if node.Contract == "WorkingPixels" || node.Contract == "WorkingAlpha" || node.Contract == "DenseWarpField" {
		media = "application/octet-stream"
	}
	return objectstore.Object{Key: node.Ref.ArtifactID.String(), VersionID: version, SHA256: node.Ref.SHA256, Bytes: size, ContentType: media}, nil
}

func TestCatalogDetectorTaskRefusesRevocationAndMutation(t *testing.T) {
	raw, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var images []json.RawMessage
	if err := json.Unmarshal(raw, &images); err != nil {
		t.Fatal(err)
	}
	body := images[0]
	image, err := review.ParseWorkingImage(body)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	canvas, err := canonical.Hash(image.Canvas)
	if err != nil {
		t.Fatal(err)
	}
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}, CanvasHash: canvas}
	prepared, err := v2input.PrepareSemanticFaceDetectParams(context.Background(), binding, body, strings.Repeat("a", 64), .8, 64)
	if err != nil {
		t.Fatal(err)
	}
	sum = sha256.Sum256(prepared.Params)
	params := v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	tenant, workflow := uuid.New(), uuid.New()
	for _, mode := range []string{"valid", "revoke", "mutate", "size", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			catalog := &detectorCatalogFixture{sizes: map[uuid.UUID]int64{params.ArtifactID: int64(len(prepared.Params))}, calls: map[uuid.UUID]int{}, mode: mode, who: who, tenant: tenant}
			for _, entry := range prepared.Inputs {
				catalog.sizes[entry.Ref.ArtifactID] = 1024
			}
			catalog.sizes[binding.Source.ArtifactID] = int64(len(body))
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancel" {
				cancel()
			}
			frozen, err := prepareCatalogDetectorTask(ctx, catalog, who, tenant, workflow, 0, binding, body, params, strings.Repeat("a", 64), .8, 64, v2input.SemanticQualifiedCosts{VRAMBytes: 1 << 30}, 1<<40, 12<<30, 1<<40, 1<<20)
			if mode == "valid" {
				if err != nil {
					t.Fatal(err)
				}
				if _, err := frozen.frozen.DraftTask(); err != nil {
					t.Fatal(err)
				}
				paramsPrepared := sourceDetectorPreparation{document: uuid.New(), frozen: frozen.frozen, inputs: frozen.inputs}
				if _, err := marshalDetectorParamsPin(context.Background(), "bucket", paramsPrepared); err != nil {
					t.Fatal(err)
				}
				paramsPrepared.document = uuid.Nil
				if _, err := marshalDetectorParamsPin(context.Background(), "bucket", paramsPrepared); err == nil {
					t.Fatal("missing document owner accepted")
				}
				var retained []v2draft.InputEvidence
				if err := json.Unmarshal([]byte(frozen.inputs), &retained); err != nil || len(retained) != len(prepared.Inputs)+1 {
					t.Fatal("catalog evidence lost", err)
				}
				if _, err := decodeDetectorInputEvidence(context.Background(), frozen.frozen, frozen.inputs); err != nil {
					t.Fatal(err)
				}
				truncated, err := canonical.Marshal(retained[:len(retained)-1])
				if err != nil {
					t.Fatal(err)
				}
				if _, err := decodeDetectorInputEvidence(context.Background(), frozen.frozen, string(truncated)); err == nil {
					t.Fatal("missing retained input accepted")
				}
				duplicate := append(append([]v2draft.InputEvidence{}, retained...), retained[0])
				duplicated, err := canonical.Marshal(duplicate)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := decodeDetectorInputEvidence(context.Background(), frozen.frozen, string(duplicated)); err == nil {
					t.Fatal("duplicate retained input accepted")
				}
				var sourcePins []sourcePreparationPin
				for _, contract := range []string{"WorkingImage", "WorkingPixels", "WorkingAlpha", "DenseWarpField"} {
					for _, entry := range retained {
						if entry.Contract != contract {
							continue
						}
						node := v2draft.LayerRef{Ref: entry.Ref, Contract: entry.Contract}
						if contract == "WorkingImage" {
							node.CanvasHash = binding.CanvasHash
						} else {
							node.ParentRef = binding.Source
						}
						sourcePins = append(sourcePins, sourcePreparationPin{Node: node, Object: entry.Object})
					}
				}
				sourceBody, err := canonical.Marshal(sourcePins)
				if err != nil {
					t.Fatal(err)
				}
				bound := sourceDetectorPreparation{frozen: frozen.frozen, inputs: frozen.inputs, source: sourcePreparationProof{binding: binding, pins: string(sourceBody)}}
				if err := verifyDetectorSourceInputPins(context.Background(), bound); err != nil {
					t.Fatal(err)
				}
				changed := append([]v2draft.InputEvidence{}, retained...)
				for i := range changed {
					if changed[i].Contract == "WorkingPixels" {
						changed[i].Object.VersionID = "other"
					}
				}
				changedBody, err := canonical.Marshal(changed)
				if err != nil {
					t.Fatal(err)
				}
				// Compose a complete synthetic parent/snapshot/policy childDraft fixture.
				snapshotID := uuid.New()
				intent := v2input.SemanticEditIntent{SchemaVersion: 1, Kind: "remove_object", Reference: binding, ReferenceMask: v2input.Ref{ArtifactID: uuid.New(), SHA256: params.SHA256}, TargetSnapshotID: snapshotID, Strength: .5, PreservePersons: true}
				intentBody, err := canonical.Marshal(intent)
				if err != nil {
					t.Fatal(err)
				}
				request, err := CompileRequest(tenant, who.UserID, "detector-plan", intentBody)
				if err != nil {
					t.Fatal(err)
				}
				_, childWorkflow, err := detectorPreparationIDs(request.RunID)
				if err != nil {
					t.Fatal(err)
				}
				childCatalog := *catalog
				childCatalog.calls = map[uuid.UUID]int{}
				child, err := prepareCatalogDetectorTask(context.Background(), &childCatalog, who, tenant, childWorkflow, 0, binding, body, params, strings.Repeat("a", 64), .8, 64, v2input.SemanticQualifiedCosts{VRAMBytes: 1 << 30}, 1<<40, 12<<30, 1<<40, 1<<20)
				if err != nil {
					t.Fatal(err)
				}
				childDraft, err := child.frozen.DraftTask()
				if err != nil {
					t.Fatal(err)
				}
				childTaskSHA, err := canonical.Hash(childDraft)
				if err != nil {
					t.Fatal(err)
				}
				project := uuid.New()
				reader := SQLTransferSourceReader{ProjectID: project, Bucket: "bucket"}
				policyBody, err := canonical.Marshal([]qualificationPolicyRow{{ID: uuid.New(), Operation: "face.detect", Version: "1", Model: strings.Repeat("a", 64), Runtime: params.SHA256, Device: params.SHA256, Dataset: params.SHA256, Backend: "cuda", Issuer: uuid.New(), Checks: []string{"quality"}, Fixtures: []string{params.SHA256}, Evidence: []string{params.SHA256}, License: true, Enabled: true, Admission: true}})
				if err != nil {
					t.Fatal(err)
				}
				policy := qualificationPolicySnapshot{tenant: tenant, project: project, actor: who.UserID, session: who.SessionID, body: string(policyBody)}
				selected, err := selectQualificationPolicies(policy, []string{"face.detect"})
				if err != nil {
					t.Fatal(err)
				}
				expectedBody, err := canonical.Marshal(selected["face.detect"])
				if err != nil {
					t.Fatal(err)
				}
				full := sourceDetectorPreparation{costs: v2input.SemanticQualifiedCosts{VRAMBytes: 1 << 30}, parent: request.RunID, document: uuid.New(), frozen: child.frozen, inputs: child.inputs, source: sourcePreparationProof{tenant: tenant, project: project, actor: who.UserID, session: who.SessionID, bucket: "bucket", binding: binding, iccSHA: strings.Repeat("c", 64), unitRGB: true, pins: string(sourceBody)}, qualification: detectorQualificationProof{tenant: tenant, project: project, actor: who.UserID, session: who.SessionID, bucket: "bucket", taskSHA: childTaskSHA, expected: string(expectedBody), report: personEvidenceObject{ref: params, contract: "QualificationReport", backend: "cuda", object: objectstore.Object{Key: "report", VersionID: "v1", SHA256: params.SHA256, Bytes: 512}}}}
				snapshot, snapshotBody, err := v2input.CompilePinnedSessionInputSnapshot(snapshotID, []v2input.SessionSnapshotItem{{Ordinal: 0, Binding: binding, WorkingImage: binding.Source, Recipe: params}})
				if err != nil {
					t.Fatal(err)
				}
				snapshotSHA, err := canonical.Hash(snapshot)
				if err != nil {
					t.Fatal(err)
				}
				intentSHA, err := canonical.Hash(intent)
				if err != nil {
					t.Fatal(err)
				}
				published := PublishedIntent{Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: intentSHA}, Inventory: CatalogInventory{Scope: CatalogScope{ProjectID: project, ItemCount: 1}, Targets: []v2input.SessionVersionBinding{binding}}}
				plan, err := reader.compileDetectorDraftPlan(context.Background(), who, request, "detector-plan", published, v2input.Ref{ArtifactID: uuid.New(), SHA256: snapshotSHA}, snapshotBody, full, policy, 16<<20)
				if err != nil {
					t.Fatal("full detector childDraft", err)
				}
                var queueConfig map[string]json.RawMessage
                if json.Unmarshal(plan.shell.Config,&queueConfig)!=nil||string(queueConfig["queue_class"])!=`"batch"`{t.Fatal("new detector queue missing")}
                oldKeyed,err:=reader.compileDetectorDraftPlanRecovery(context.Background(),who,request,"detector-plan",published,plan.anchor.snapshotRef,snapshotBody,full,policy,16<<20,true)
                if err!=nil{t.Fatal(err)}
				assertNativeTargetDetectorPlan(t, reader, &childCatalog, who, plan, body, params)
				if plan.shell.Run == request.RunID || len(plan.shell.Tasks) != 1 || plan.shell.Tasks[0].ID != childDraft.ID {
					t.Fatal("child shell lost identity")
				}
				legacy, err := reader.compileDetectorDraftPlanRecovery(context.Background(), who, request, "detector-plan", published, plan.anchor.snapshotRef, snapshotBody, full, policy, 16<<20, false)
				if err != nil || legacy.shell.ConfigSHA == plan.shell.ConfigSHA {
					t.Fatal("legacy recovery format fixture", err)
				}
				for _, original := range []detectorDraftPlan{plan, legacy, oldKeyed} {
					rebuilt, err := reader.recompileDetectorDraftPlan(context.Background(), who, original)
					if err != nil || rebuilt.seal != original.seal || rebuilt.shell.ConfigSHA != original.shell.ConfigSHA || string(rebuilt.shell.Config) != string(original.shell.Config) {
						t.Fatal("recovery changed original detector config", err)
					}
				}
				transport, packet, err := publishDetectorDraftTransport(context.Background(), plan, &proposalMemoryTransport{})
				if err != nil || packet.Count != transport.Graph.Built.Count {
					t.Fatal("childDraft transport", err)
				}
				assertNativeRegistrationBinding(t, plan.prepared.frozen, transport.Graph.Built, "face.detect", func(ctx context.Context, built v2input.Built) (json.RawMessage, error) {
					return marshalDetectorRegistrationBinding(ctx, plan, built)
				})
				assertNativeAdmissionEvidence(t, reader, who, request, plan.shell, plan.prepared.source, "face.detect", func(ctx context.Context) (json.RawMessage, error) {
					return reader.marshalDetectorAdmissionEvidence(ctx, who, plan, transport.Graph.Built)
				})
				for _, kind := range []string{"source_tenant", "source_project", "source_actor", "source_session", "source_bucket", "icc", "unit_rgb"} {
					changed := plan
					mutateNativeSourceScope(kind, &changed.prepared.source)
					changed.seal, err = detectorPlanDigest(changed)
					if err != nil {
						t.Fatal(err)
					}
					if _, err := reader.marshalDetectorAdmissionEvidence(context.Background(), who, changed, transport.Graph.Built); err == nil {
						t.Fatal("resealed invalid authority accepted", kind)
					}
				}
				assertNativeSourceLock(t, reader, who, request.TenantID, func(ctx context.Context, tx pgx.Tx) error {
					return reader.lockDetectorAdmissionSourceInTransaction(ctx, tx, who, plan, transport.Graph.Built)
				})
				assertNativeResume(t, transport.Graph.ID, transport.Graph.Built, v2input.JobTicket{RunID: plan.shell.Run, WorkflowID: plan.shell.WorkflowID, State: "queued"}, func(ctx context.Context, tx pgx.Tx) (v2input.JobTicket, error) {
					return reader.recoverAndSealDetectorInTransaction(ctx, tx, who, plan)
				})
				assertFrozenNativeRecovery(t, transport.Graph.ID, transport.Graph.Built, func(ctx context.Context, tx pgx.Tx) (v2input.Built, error) {
					return recoverDetectorTransportInTransaction(ctx, tx, who, plan)
				})
				assertNativeSeal(t, request.TenantID, plan.shell.Run, plan.shell.WorkflowID, func(ctx context.Context, tx pgx.Tx) (v2input.JobTicket, error) {
					return reader.sealDetectorPreparationInTransaction(ctx, tx, who, plan, transport.Graph.Built)
				})
				registrationRequest := request
				registrationRequest.RunID = plan.shell.Run
				for _, expectedState := range []string{"missing", "replayed", "conflict"} {
					tx := &manualLockTx{role: expectedState}
					state, err := detectorPreparationRegistrationState(context.Background(), tx, registrationRequest, childDraft, transport.Graph)
					if err != nil || state != expectedState || len(tx.calls) != 1 || len(tx.args[0]) != 13 {
						t.Fatal("registration gate packet", state, err)
					}
					args := tx.args[0]
					if args[0] != tenant || args[1] != plan.shell.Run || args[2] != childDraft.ID || args[3] != childDraft.ParamsArtifactID || args[4] != childDraft.ParamsSHA || args[5] != childDraft.InputDigest || string(args[6].(json.RawMessage)) != string(childDraft.ExecutionProfile) ||
						string(args[7].(json.RawMessage)) != string(packet.Rows) || string(args[8].(json.RawMessage)) != string(packet.Manifest) || string(args[9].(json.RawMessage)) != string(packet.Chunks) || string(args[10].(json.RawMessage)) != string(packet.Direct) || args[11] != packet.Count || args[12] != packet.EntriesSHA {
						t.Fatal("registration changed immutable transport or parent scope")
					}
				}
				for _, kind := range []string{"reservation", "config", "policy", "inputs"} {
					modified := plan
					switch kind {
					case "reservation":
						modified.shell.Bytes++
					case "config":
						var config map[string]any
						if err := json.Unmarshal(plan.shell.Config, &config); err != nil {
							t.Fatal(err)
						}
						config["unexpected"] = true
						modified.shell.Config, err = canonical.Marshal(config)
						if err != nil {
							t.Fatal(err)
						}
					case "policy":
						modified.policy.body += " "
					case "inputs":
						modified.prepared.inputs += " "
					}
					if err := verifyDetectorPlanSeal(context.Background(), modified); !errors.Is(err, v2input.ErrConflict) {
						t.Fatal("plan mutation accepted", kind, err)
					}
				}
				bad := plan
				bad.shell.Tasks = append([]v2input.DraftTask{}, plan.shell.Tasks...)
				bad.shell.Tasks[0].ParamsSHA = strings.Repeat("f", 64)
				if _, _, err := publishDetectorDraftTransport(context.Background(), bad, &proposalMemoryTransport{}); err == nil {
					t.Fatal("mutated shell task published")
				}
				bound.inputs = string(changedBody)
				if err := verifyDetectorSourceInputPins(context.Background(), bound); !errors.Is(err, v2input.ErrConflict) {
					t.Fatal("different source object version accepted", err)
				}
				for _, entry := range retained {
					if entry.Object.VersionID != "v1" || entry.Object.SHA256 != entry.Ref.SHA256 || entry.Object.Bytes != catalog.sizes[entry.Ref.ArtifactID] {
						t.Fatal("catalog pins changed")
					}
				}
				draft, err := frozen.frozen.DraftTask()
				if err != nil {
					t.Fatal(err)
				}
				taskSHA, err := canonical.Hash(draft)
				if err != nil {
					t.Fatal(err)
				}
				proof := detectorQualificationProof{taskSHA: taskSHA}
				if err := verifyDetectorTaskQualificationBinding(frozen.frozen, proof); err != nil {
					t.Fatal(err)
				}
				proof.taskSHA = strings.Repeat("f", 64)
				if err := verifyDetectorTaskQualificationBinding(frozen.frozen, proof); !errors.Is(err, v2input.ErrConflict) {
					t.Fatal("changed task binding accepted", err)
				}
				for _, count := range catalog.calls {
					if count != 2 {
						t.Fatal("catalog authority not rechecked")
					}
				}
			} else {
				if err == nil {
					t.Fatal("invalid catalog accepted")
				}
				if _, draftErr := frozen.frozen.DraftTask(); draftErr == nil {
					t.Fatal("partial task escaped")
				}
				if mode == "cancel" && (!errors.Is(err, context.Canceled) || len(catalog.calls) != 0) {
					t.Fatal("cancelled task performed IO")
				}
			}
		})
	}
}
