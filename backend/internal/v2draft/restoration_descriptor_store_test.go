package v2draft

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
)

type restorationStoreDB struct{ tx *restorationStoreTx }

func (d restorationStoreDB) Begin(context.Context) (pgx.Tx, error) { return d.tx, nil }

type restorationStoreRow struct{ value any }

func (r restorationStoreRow) Scan(dest ...any) error {
	switch p := dest[0].(type) {
	case *string:
		*p = r.value.(string)
	case *bool:
		*p = r.value.(bool)
	case *[]byte:
		*p = append([]byte(nil), r.value.([]byte)...)
	}
	return nil
}

type restorationStoreTx struct {
	pgx.Tx
	role     string
	accepted bool
	read     []byte
	queries  []string
	args     []any
	tenant   string
	commits  int
}

func (t *restorationStoreTx) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	t.queries = append(t.queries, query)
	if len(args) > 0 {
		t.args = args
	}
	if query == "SELECT current_user" {
		return restorationStoreRow{t.role}
	}
	if strings.Contains(query, "save_v28_restoration_descriptor") ||
		strings.Contains(query, "begin_v28_restoration_draft") ||
		strings.Contains(query, "begin_v28_reflection_draft") ||
		strings.Contains(query, "begin_v28_open_eyes_draft") ||
		strings.Contains(query, "save_v28_restoration_params_intent") ||
		strings.Contains(query, "stage_v28_restoration_task") ||
		strings.Contains(query, "seal_v28_restoration_graph") {
		return restorationStoreRow{t.accepted}
	}
	return restorationStoreRow{t.read}
}
func (t *restorationStoreTx) Exec(_ context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
	t.tenant = args[0].(string)
	return pgconn.CommandTag{}, nil
}
func (t *restorationStoreTx) Commit(context.Context) error   { t.commits++; return nil }
func (t *restorationStoreTx) Rollback(context.Context) error { return nil }

func TestRestorationDescriptorStoreBindsSchedulerAndExactBytes(t *testing.T) {
	ref := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)} }
	entries := make([]v2input.Entry, 6)
	for i := range entries {
		entries[i] = v2input.Entry{Ref: ref(), Contract: "NpyArray"}
	}
	prepared := RestorationResolvedDraft{Draft: v2portrait.RestorationDraft{
		Task: v2input.DraftTask{ID: uuid.New(), Operation: "image.reflection"}, Entries: entries},
		Origin: RestorationManualOrigin{Root: ref(), Params: ref(), TargetPixel: ref(), ProtectedPixel: ref()}}
	params := []byte(`{"schema_version":1}`)
	body, err := prepared.Descriptor(params)
	if err != nil {
		t.Fatal(err)
	}
	tx := &restorationStoreTx{role: "photo_scheduler", accepted: true, read: body}
	store := SQLRestorationDescriptorStore{DB: restorationStoreDB{tx}}
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	tenant, run := uuid.New(), uuid.New()
	if err := store.Save(context.Background(), who, tenant, run, prepared, params); err != nil {
		t.Fatal(err)
	}
	if len(tx.args) != 6 || tx.args[4] != string(body) || tx.args[5] != string(params) ||
		tx.tenant != tenant.String() || tx.commits != 1 {
		t.Fatal("save did not bind exact descriptor")
	}
	if err := store.Stage(context.Background(), who, tenant, run, prepared, params); err != nil ||
		!strings.Contains(tx.queries[len(tx.queries)-1], "stage_v28_restoration_task") || tx.commits != 2 {
		t.Fatal("blocked staging did not bind exact descriptor", err)
	}
	read, err := store.Read(context.Background(), who, tenant, run)
	if err != nil || string(read) != string(body) || tx.commits != 3 {
		t.Fatal("read did not return canonical bytes", err)
	}
	if err := store.AdmitRegistered(context.Background(), who, tenant, run, prepared, params); err != nil ||
		!strings.Contains(tx.queries[len(tx.queries)-1], "seal_v28_restoration_graph") || tx.commits != 4 {
		t.Fatal("dedicated graph admission was not used", err)
	}
	tx.role = "photo_api"
	tx.queries = nil
	if _, err := store.Read(context.Background(), who, tenant, run); err == nil || len(tx.queries) != 1 {
		t.Fatal("non-scheduler reached descriptor read")
	}
}

func TestRestorationAdmissionTicketBindsRequestAndRole(t *testing.T) {
	tenant, project, shoot, run, workflow, snapshot := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	ordinal := 2
	task, err := v2input.TaskID(workflow, "portrait.glare", ordinal)
	if err != nil {
		t.Fatal(err)
	}
	proposal := []byte(`{"mode":"donor","schema_version":1}`)
	profile := []byte(`{"backend":"cpu"}`)
	source, donor := uuid.New(), uuid.New()
	ticket := []byte(`{"run_id":"` + run.String() + `","state":"queued","task_id":"` +
		task.String() + `","workflow_id":"` + workflow.String() + `"}`)
	tx := &restorationStoreTx{role: "photo_scheduler", read: ticket}
	store := SQLRestorationDescriptorStore{DB: restorationStoreDB{tx}}
	got, err := store.ReadAdmissionTicketForWorkflow(context.Background(), who,
		tenant, project, shoot, run, workflow, snapshot, ordinal, proposal, profile, source, donor)
	if err != nil || got.TaskID != task || got.RunID != run || tx.commits != 1 ||
		len(tx.args) != 11 || tx.args[4] != project || tx.args[5] != shoot ||
		tx.args[6] != snapshot || tx.args[7] != string(proposal) ||
		tx.args[8] != string(profile) || tx.args[9] != source || tx.args[10] != donor {
		t.Fatal("ticket recovery lost exact request binding", err)
	}
	tx.read = nil
	if _, err := store.ReadAdmissionTicketForWorkflow(context.Background(), who,
		tenant, project, shoot, run, workflow, snapshot, ordinal, proposal, profile, source, donor); err != v2input.ErrExpired {
		t.Fatal("missing or revoked ticket was accepted", err)
	}
	tx.role = "photo_api"
	if _, err := store.ReadAdmissionTicketForWorkflow(context.Background(), who,
		tenant, project, shoot, run, workflow, snapshot, ordinal, proposal, profile, source, donor); err != v2input.ErrInvalid {
		t.Fatal("non-scheduler read admission ticket", err)
	}
}
