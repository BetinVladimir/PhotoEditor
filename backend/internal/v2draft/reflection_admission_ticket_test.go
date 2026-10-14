package v2draft

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func TestReflectionAdmissionTicketRequiresExactCommittedIdentity(t *testing.T) {
	sha := strings.Repeat("a", 64)
	ref := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: sha} }
	run, workflow := uuid.New(), uuid.New()
	task, err := v2input.TaskID(workflow, "image.reflection", 3)
	if err != nil {
		t.Fatal(err)
	}
	ticket := RestorationAdmissionTicket{RunID: run, WorkflowID: workflow,
		TaskID: task, State: "queued"}
	result, err := canonical.Marshal(ticket)
	if err != nil {
		t.Fatal(err)
	}
	tx := &restorationStoreTx{role: "photo_scheduler", read: result}
	store := SQLRestorationDescriptorStore{DB: restorationStoreDB{tx}}
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	target, protected := ref(), ref()
	call := func() (RestorationAdmissionTicket, error) {
		return store.ReadReflectionAdmissionTicket(context.Background(), who,
			uuid.New(), uuid.New(), run, workflow, uuid.New(), 3,
			[]byte(`{}`), []byte(`{}`), target, protected)
	}
	got, err := call()
	if err != nil || got != ticket || tx.commits != 1 || len(tx.args) != 10 ||
		!strings.Contains(tx.queries[len(tx.queries)-1],
			"read_v28_reflection_admission_ticket") {
		t.Fatal("committed reflection graph receipt refused", err)
	}
	changed := ticket
	changed.TaskID = uuid.New()
	tx.read, err = canonical.Marshal(changed)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := call(); err != v2input.ErrInvalid || got != (RestorationAdmissionTicket{}) ||
		tx.commits != 1 {
		t.Fatal("substituted reflection task acknowledged", err)
	}
	tx.role = "photo_api"
	tx.queries = nil
	if got, err := call(); err != v2input.ErrInvalid || got != (RestorationAdmissionTicket{}) ||
		len(tx.queries) != 1 {
		t.Fatal("non-scheduler role reached receipt function", err)
	}
}
