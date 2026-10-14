package provenance

import (
	"context"
 "encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
)

type signingDraftResumeTx struct {
	pgx.Tx
	present  bool
	conflict bool
	calls    []string
}
type signingDraftResumeRow struct {
	text string
	flag bool
}

func (r signingDraftResumeRow) Scan(values ...any) error {
	switch p := values[0].(type) {
	case *string:
		*p = r.text
	case *bool:
		*p = r.flag
	default:
		return errors.New("unexpected scan")
	}
	return nil
}
func (s *signingDraftResumeTx) QueryRow(_ context.Context, query string, _ ...any) pgx.Row {
	s.calls = append(s.calls, query)
	if strings.Contains(query, "pin_v31_signing_pending") {
		outcome := "pending"
		if s.conflict {
			outcome = "conflict"
		}
		return signingDraftResumeRow{text: outcome}
	}
	if strings.Contains(query, "v31_signing_draft_present") {
		return signingDraftResumeRow{flag: s.present}
	}
	return signingDraftResumeRow{flag: true}
}

func TestEnsureSigningDraftResumesExactBlockedDraft(t *testing.T) {
	config, params, now := signingRegistryFixture(t)
	registry, err := NewSigningPolicyRegistry(config, &signingAuthorityStub{})
	if err != nil {
		t.Fatal(err)
	}
	body, lineage := contractBytes(t, SigningLineage{Schema: 1, SourceHashes: []string{params.Export.SHA256}, Actions: []SigningAction{}})
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	tid, project, export := uuid.New(), uuid.New(), uuid.New()
	source := &signingSourceStub{evidence: SigningExportEvidence{Project: project, ExportID: export, Export: params.Export, MIME: "image/jpeg", Lineage: lineage, LineageBody: body}}
	intent, err := PrepareSigningIntent(context.Background(), who, tid, export, params.PolicyID, "resume", source, registry, now)
	if err != nil {
		t.Fatal(err)
	}
	task, err := CompileSigningTask(context.Background(), intent, registry, "image/jpeg", SigningTaskResources{ContractsSHA: strings.Repeat("d", 64), RAMBytes: 32 << 20, ScratchBytes: 1 << 30, OutputBytes: 528 << 20, MaxInputJSONBytes: 16 << 20}, now)
	if err != nil {
		t.Fatal(err)
	}
	prepared := PreparedSigningActivation{intent: intent, compiled: task, mime: "image/jpeg"}
	ready := ReadySigningExport{Tenant: tid, Project: project, ExportID: export, Version: uuid.New(), Task: uuid.New(), Run: uuid.New(), Ref: params.Export, Object: objectstore.Object{Key: "source", VersionID: "v1", SHA256: params.Export.SHA256, Bytes: 10, ContentType: "image/jpeg"}}

 for _, historical := range []bool{false,true} {
  candidate := prepared
  candidate.intent.legacyQueueOmitted = historical
  pendingRaw,err:=compileSigningPending(who,ready,candidate,"test-bucket");if err!=nil{t.Fatal(err)}
  restored,compiled,restoredReady,err:=RestoreSigningPending(context.Background(),who,tid,export,"resume",intent.requestSHA,pendingRaw,registry,now);if err!=nil{t.Fatal(err)}
  replay:=PreparedSigningActivation{intent:restored,compiled:compiled,mime:"image/jpeg"}
  replayRaw,err:=compileSigningPending(who,restoredReady,replay,"test-bucket");if err!=nil||string(replayRaw)!=string(pendingRaw){t.Fatal("pending replay changed",err)}
  configRaw,_,err:=signingDraftDocuments(who,restoredReady,replay,"test-bucket");if err!=nil{t.Fatal(err)}
  var config map[string]json.RawMessage
  if err:=json.Unmarshal(configRaw,&config);err!=nil{t.Fatal(err)}
  value,present:=config["queue_class"];if present==historical||(!historical&&string(value)!=`"batch"`){t.Fatal("queue choice changed")}
  var invalid map[string]json.RawMessage
  if err:=json.Unmarshal(pendingRaw,&invalid);err!=nil{t.Fatal(err)}
  for _,foreign:=range []string{`"interactive"`,`"training"`,`null`,`""`}{
   invalid["queue_class"]=json.RawMessage(foreign)
   body,err:=canonical.Marshal(invalid);if err!=nil{t.Fatal(err)}
   if _,_,_,err:=RestoreSigningPending(context.Background(),who,tid,export,"resume",intent.requestSHA,body,registry,now);err==nil{t.Fatal("foreign signing queue accepted",foreign)}
  }
 }
	for _, mode := range []string{"new", "existing", "conflict"} {
		t.Run(mode, func(t *testing.T) {
			tx := &signingDraftResumeTx{present: mode == "existing", conflict: mode == "conflict"}
			err := ensureSigningDraft(context.Background(), tx, who, ready, prepared, "test-bucket")
			if mode == "conflict" {
				if !errors.Is(err, artifactjson.ErrConflict) || len(tx.calls) != 1 {
					t.Fatalf("conflict: %v %d", err, len(tx.calls))
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			expected := 4
			if mode == "existing" {
				expected = 2
			}
			if len(tx.calls) != expected {
				t.Fatalf("queries %d want %d", len(tx.calls), expected)
			}
		})
	}
}

func TestSignExportRejectsUnconfiguredAndCancelledBeforeIO(t *testing.T) {
	service := SQLSignExportService{}
	if _, err := service.SignExport(context.Background(), auth.Identity{}, uuid.New(), uuid.New(), uuid.New(), "key"); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.SignExport(ctx, auth.Identity{}, uuid.New(), uuid.New(), uuid.New(), "key"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
