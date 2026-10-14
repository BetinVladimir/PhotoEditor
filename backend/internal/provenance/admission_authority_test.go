package provenance

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

type signingAdmissionRow struct {
	accepted bool
	err      error
}

func (r signingAdmissionRow) Scan(dst ...any) error {
	if r.err != nil {
		return r.err
	}
	*(dst[0].(*bool)) = r.accepted
	return nil
}

type signingAdmissionTx struct {
	calls    int
	accepted bool
	err      error
	args     []any
}

func (s *signingAdmissionTx) QueryRow(_ context.Context, _ string, args ...any) pgx.Row {
	s.calls++
	s.args = args
	return signingAdmissionRow{s.accepted, s.err}
}
func TestSigningAdmissionChecksPrivateReceiptBeforeSQL(t *testing.T) {
	for _, mode := range []string{"valid", "stale", "sql_error", "actor", "session", "source", "project", "params", "inputs", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			config, params, now := signingRegistryFixture(t)
			registry, err := NewSigningPolicyRegistry(config, &signingAuthorityStub{})
			if err != nil {
				t.Fatal(err)
			}
			body, lineage := contractBytes(t, SigningLineage{Schema: 1, SourceHashes: []string{params.Export.SHA256}, Actions: []SigningAction{}})
			export := uuid.New()
			lineage.ArtifactID = uuid.NewSHA1(export, []byte("signing:lineage:"+lineage.SHA256))
			who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
			project := uuid.New()
			tid := uuid.New()
			intent, err := PrepareSigningIntent(context.Background(), who, tid, export, params.PolicyID, "admission", &signingSourceStub{evidence: SigningExportEvidence{Project: project, ExportID: export, Export: params.Export, MIME: "image/jpeg", Lineage: lineage, LineageBody: body}}, registry, now)
			if err != nil {
				t.Fatal(err)
			}
			compiled, err := CompileSigningTask(context.Background(), intent, registry, "image/jpeg", SigningTaskResources{ContractsSHA: strings.Repeat("d", 64), RAMBytes: 32 << 20, ScratchBytes: 1 << 30, OutputBytes: (512 << 20) + (16 << 20), MaxInputJSONBytes: 16 << 20}, now)
			if err != nil {
				t.Fatal(err)
			}
			receipt := PreparedSigningActivation{intent: intent, compiled: compiled, mime: "image/jpeg"}
			ready := ReadySigningExport{Tenant: tid, Project: project, ExportID: export, Version: uuid.New(), Task: uuid.New(), Run: uuid.New(), Ref: params.Export, Object: objectstore.Object{Key: "source", VersionID: "v1", SHA256: params.Export.SHA256, Bytes: 10, ContentType: "image/jpeg"}}
			tx := &signingAdmissionTx{accepted: true}
			ctx := context.Background()
			cause := errors.New("database unavailable")
			switch mode {
			case "stale":
				tx.accepted = false
			case "sql_error":
				tx.err = cause
			case "actor":
				who.UserID = uuid.New()
			case "session":
				who.SessionID = uuid.New()
			case "source":
				ready.Ref.ArtifactID = uuid.New()
			case "project":
				ready.Project = uuid.New()
			case "params":
				receipt.compiled.body = []byte(`{}`)
			case "inputs":
				receipt.compiled.inputs = receipt.compiled.Inputs()
				receipt.compiled.inputs[0].Contract = "wrong"
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if mode == "valid" {
				raw, err := compileSigningPending(who, ready, receipt, "test-bucket")
				if err != nil {
					t.Fatal(err)
				}
				var saved signingPendingDocument
				if json.Unmarshal(raw, &saved) != nil || saved.Run != intent.RunID() || saved.Params != string(intent.ParamsBytes()) || saved.RequestSHA != intent.RequestSHA() || len(saved.Inputs) != 3 {
					t.Fatal("pending evidence lost pins")
				}
				restoredWho := who
				restoredWho.SessionID = uuid.New()
				restored, restoredTask, restoredReady, err := RestoreSigningPending(ctx, restoredWho, tid, export, "admission", intent.RequestSHA(), raw, registry, now)
				if err != nil || restored.WorkflowID() != intent.WorkflowID() || restored.session != restoredWho.SessionID || restoredTask.Task().ID != compiled.Task().ID || restoredReady != ready {
					t.Fatal("pending recovery failed", err)
				}
				copy := restored.ParamsBytes()
				copy[0] = 'x'
				if restored.ParamsBytes()[0] != '{' {
					t.Fatal("restored intent aliases caller")
				}
				for _, change := range []string{"actor", "key", "request_hash", "task", "profile", "inputs", "source", "params", "lineage", "schema", "unknown", "expired", "cancelled"} {
					altered := saved
					altered.Inputs = append([]v2input.Entry(nil), saved.Inputs...)
					recoveryWho, recoveryKey, recoverySHA, recoveryNow, recoveryCtx := who, "admission", intent.RequestSHA(), now, ctx
					switch change {
					case "actor":
						recoveryWho.UserID = uuid.New()
					case "key":
						recoveryKey = "other-key"
					case "request_hash":
						recoverySHA = strings.Repeat("f", 64)
					case "task":
						altered.Task.ID = uuid.New()
					case "profile":
						altered.Task.ExecutionProfile = json.RawMessage(`{}`)
					case "inputs":
						altered.Inputs[0].Contract = "wrong"
					case "source":
						altered.Source = json.RawMessage(`{}`)
					case "params":
						altered.Params = "{}"
					case "lineage":
						altered.Lineage = "{}"
					case "schema":
						altered.Schema = 2
					case "expired":
						recoveryNow = now.AddDate(20, 0, 0)
					case "cancelled":
						var cancel context.CancelFunc
						recoveryCtx, cancel = context.WithCancel(ctx)
						cancel()
					}
					data, err := canonical.Marshal(altered)
					if err != nil {
						t.Fatal(err)
					}
					if change == "unknown" {
						data = append([]byte(`{"unknown":true,`), data[1:]...)
						data, err = canonical.Bytes(data)
						if err != nil {
							t.Fatal(err)
						}
					}
					recovered, _, _, err := RestoreSigningPending(recoveryCtx, recoveryWho, tid, export, recoveryKey, recoverySHA, data, registry, recoveryNow)
					if err == nil || recovered.WorkflowID() != uuid.Nil {
						t.Fatal("invalid recovery evidence accepted", change)
					}
				}
				repository := SQLSigningParamsRepository{DB: &pgxpool.Pool{}, Who: who, Ready: ready, Prepared: receipt, Bucket: "test-bucket"}
				paramsRef, paramsBody := receipt.compiled.ParamsDocument()
				scope := artifactjson.Scope{Tenant: tid, Run: intent.RunID(), Owner: intent.TaskID(), Purpose: "task_params"}
				if actual, err := repository.validate(scope, paramsRef.SHA256, int64(len(paramsBody))); err != nil || actual != paramsRef {
					t.Fatal("Params scope rejected", err)
				}
				for _, change := range []string{"tenant", "run", "owner", "purpose", "lease", "local", "hash", "bytes"} {
					changed, sha, size := scope, paramsRef.SHA256, int64(len(paramsBody))
					switch change {
					case "tenant":
						changed.Tenant = uuid.New()
					case "run":
						changed.Run = uuid.New()
					case "owner":
						changed.Owner = uuid.New()
					case "purpose":
						changed.Purpose = "recipe"
					case "lease":
						changed.AssemblyLease = uuid.New()
					case "local":
						changed.LocalPortrait = true
					case "hash":
						sha = strings.Repeat("f", 64)
					case "bytes":
						size++
					}
					if _, err := repository.validate(changed, sha, size); err == nil {
						t.Fatal("foreign Params scope accepted", change)
					}
				}
				documents := &signingTransportDocuments{}
				transport, err := PublishSigningInputs(ctx, who, ready, receipt, "test-bucket", documents)
				if err != nil || transport.Built().Count != 3 || len(transport.Built().Chunks) != 1 || transport.Built().SHA256 != compiled.Task().InputDigest || len(documents.scopes) != 2 {
					t.Fatal("signer transport publication failed", err)
				}
				for index, scope := range documents.scopes {
					purpose := "v2_input_chunk_0"
					if index == 1 {
						purpose = "v2_input_manifest"
					}
					if scope.Tenant != tid || scope.Run != intent.RunID() || scope.Owner != intent.TaskID() || scope.Purpose != purpose {
						t.Fatal("foreign transport storage scope")
					}
				}
				built := transport.Built()
				built.Chunks[0].ArtifactID = uuid.New()
				entries := transport.Entries()
				entries[0].Contract = "wrong"
				if transport.Built().Chunks[0].ArtifactID == built.Chunks[0].ArtifactID || transport.Entries()[0].Contract == "wrong" {
					t.Fatal("transport aliases returned state")
				}
				for _, transportMode := range []string{"hash", "alias", "cancelled"} {
					docs := &signingTransportDocuments{mode: transportMode, alias: compiled.params.ArtifactID}
					transportCtx := ctx
					if transportMode == "cancelled" {
						var cancel context.CancelFunc
						transportCtx, cancel = context.WithCancel(ctx)
						cancel()
					}
					if _, err := PublishSigningInputs(transportCtx, who, ready, receipt, "test-bucket", docs); err == nil {
						t.Fatal("invalid transport accepted", transportMode)
					}
					if transportMode == "cancelled" && len(docs.scopes) != 0 {
						t.Fatal("cancelled publication reached storage")
					}
				}
				for _, activationMode := range []string{"valid", "scope", "batch", "seal", "activation", "sql_error", "cancelled"} {
					activationTx := &signingActivationTx{}
					activationCtx := ctx
					switch activationMode {
					case "scope":
						activationTx.failAt = 1
					case "batch":
						activationTx.failAt = 2
					case "seal":
						activationTx.failAt = 3
					case "activation":
						activationTx.failAt = 4
					case "sql_error":
						activationTx.failAt = 4
						activationTx.err = cause
					case "cancelled":
						var cancel context.CancelFunc
						activationCtx, cancel = context.WithCancel(ctx)
						cancel()
					}
					ticket, err := ActivateSigningInTransaction(activationCtx, activationTx, who, ready, receipt, transport, "test-bucket")
					if activationMode == "valid" {
						if err != nil || ticket.RunID != intent.RunID() || ticket.WorkflowID != intent.WorkflowID() || ticket.State != "queued" || activationTx.calls != 4 {
							t.Fatal("activation coordination failed", err)
						}
					} else if err == nil || ticket.RunID != uuid.Nil {
						t.Fatal("failed activation exposed ticket")
					}
					if activationMode == "cancelled" && activationTx.calls != 0 {
						t.Fatal("cancelled activation reached SQL")
					}
					if activationTx.failAt > 0 && activationTx.calls != activationTx.failAt {
						t.Fatal("activation continued after failure")
					}
				}
				for _, materialMode := range []string{"valid", "stale", "sql_error"} {
					materialTx := &signingAdmissionTx{accepted: materialMode == "valid"}
					if materialMode == "sql_error" {
						materialTx.err = cause
					}
					err := MaterializeSigningTask(ctx, materialTx, who, ready, receipt, "test-bucket")
					if materialMode == "valid" {
						if err != nil || materialTx.calls != 1 || len(materialTx.args) != 6 || materialTx.args[3] != "admission" {
							t.Fatal("materialization scope failed", err)
						}
					} else if err == nil {
						t.Fatal("materialization failure accepted")
					}
				}
				for _, draftMode := range []string{"created", "pending", "conflict", "stale", "sql_error"} {
					draftTx := &signingDraftTx{outcome: draftMode, accepted: true}
					if draftMode == "stale" {
						draftTx.outcome = "pending"
						draftTx.accepted = false
					}
					if draftMode == "sql_error" {
						draftTx.outcome = "pending"
						draftTx.err = cause
					}
					err := PrepareSigningDraft(ctx, draftTx, who, ready, receipt, "test-bucket")
					if draftMode == "created" || draftMode == "pending" {
						if err != nil || draftTx.calls != 2 {
							t.Fatal("draft persistence failed", err)
						}
					} else if err == nil {
						t.Fatal("draft failure accepted")
					}
					if draftMode == "conflict" && draftTx.calls != 1 {
						t.Fatal("conflict reached draft creation")
					}
				}
				for _, outcome := range []string{"created", "pending", "conflict", "stale", "expired", "completed", "invalid", "unknown"} {
					pendingTx := &signingPendingTx{outcome: outcome}
					got, err := PinSigningPending(ctx, pendingTx, who, ready, receipt, "test-bucket")
					if outcome == "created" || outcome == "pending" {
						if err != nil || got != outcome {
							t.Fatal("pending write failed", err)
						}
					} else if err == nil || got != "" {
						t.Fatal("invalid pending outcome accepted")
					}
					wantCalls := 1
					if outcome == "stale" {
						wantCalls = 2
					}
					if pendingTx.calls != wantCalls || (outcome != "stale" && (len(pendingTx.args) != 6 || pendingTx.args[0] != tid || pendingTx.args[3] != "admission")) {
						t.Fatal("pending scope lost")
					}
					if outcome == "stale" {
						pendingTx = &signingPendingTx{outcome: "stale", unavailable: true}
						_, err = PinSigningPending(ctx, pendingTx, who, ready, receipt, "test-bucket")
						if domain.AsError(err).Code != "SIGNING_UNAVAILABLE" || pendingTx.calls != 2 || len(pendingTx.args) != 4 || pendingTx.args[0] != tid {
							t.Fatal("live adopter outage was not classified", err)
						}
					}
				}
			}
			err = CheckSigningAdmission(ctx, tx, who, ready, receipt, "test-bucket")
			if mode == "valid" {
				if err != nil || tx.calls != 1 || len(tx.args) != 7 || tx.args[0] != tid || tx.args[3] != export {
					t.Fatal("SQL admission binding failed", err)
				}
			} else {
				if err == nil {
					t.Fatal("invalid authority accepted")
				}
				if mode == "stale" && !errors.Is(err, artifactjson.ErrStale) {
					t.Fatal("stale error lost")
				}
				if mode == "sql_error" && !errors.Is(err, cause) {
					t.Fatal("SQL error lost")
				}
			}
			if mode != "valid" && mode != "stale" && mode != "sql_error" && tx.calls != 0 {
				t.Fatal("invalid receipt reached SQL")
			}
		})
	}
}

type signingPendingRow struct{ outcome string }

func (r signingPendingRow) Scan(dst ...any) error { *(dst[0].(*string)) = r.outcome; return nil }

type signingPendingTx struct {
	calls       int
	outcome     string
	args        []any
	unavailable bool
}

func (s *signingPendingTx) QueryRow(_ context.Context, _ string, args ...any) pgx.Row {
	s.calls++
	s.args = args
	if s.calls == 2 {
		return signingAdmissionRow{accepted: s.unavailable}
	}
	return signingPendingRow{s.outcome}
}

type signingDraftTx struct {
	calls    int
	outcome  string
	accepted bool
	err      error
}

func (s *signingDraftTx) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	s.calls++
	if s.calls == 1 {
		return signingPendingRow{s.outcome}
	}
	return signingAdmissionRow{accepted: s.accepted, err: s.err}
}

type signingTransportDocuments struct {
	scopes []artifactjson.Scope
	mode   string
	alias  uuid.UUID
}

func (s *signingTransportDocuments) Publish(_ context.Context, scope artifactjson.Scope, raw []byte) (artifactjson.Ref, error) {
	s.scopes = append(s.scopes, scope)
	ref := artifactjson.Ref{ID: uuid.New(), SHA256: bodyDigest(raw)}
	if s.mode == "hash" {
		ref.SHA256 = strings.Repeat("f", 64)
	}
	if s.mode == "alias" {
		ref.ID = s.alias
	}
	return ref, nil
}

type signingActivationTx struct {
	pgx.Tx
	calls, failAt int
	err           error
}

func (s *signingActivationTx) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	s.calls++
	return signingActivationRow{batch: s.calls == 2, fail: s.calls == s.failAt, err: s.err}
}

type signingActivationRow struct {
	batch, fail bool
	err         error
}

func (r signingActivationRow) Scan(dst ...any) error {
	if r.fail && r.err != nil {
		return r.err
	}
	if r.batch {
		value := 3
		if r.fail {
			value = 0
		}
		*(dst[0].(*int)) = value
	} else {
		*(dst[0].(*bool)) = !r.fail
	}
	return nil
}
