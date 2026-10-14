package interchange

import (
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"strings"
	"testing"
)

type jobReadRow struct {
	raw    []byte
	cancel context.CancelFunc
}

func (r jobReadRow) Scan(dest ...any) error {
	*dest[0].(*[]byte) = r.raw
	if r.cancel != nil {
		r.cancel()
	}
	return nil
}

type jobReadTx struct {
	raw    []byte
	args   []any
	cancel context.CancelFunc
}

func (t *jobReadTx) QueryRow(_ context.Context, _ string, args ...any) pgx.Row {
	t.args = args
	return jobReadRow{raw: t.raw, cancel: t.cancel}
}
func TestReadInterchangeBindsCurrentIdentityAndReturnsOwnedBytes(t *testing.T) {
	tid, who, _, _, _ := pendingFixture()
	job := uuid.New()
	project := uuid.New()
	valid := fmt.Sprintf(`{"id":"%s","revision":"1","document":null,"data":{"resource_type":"interchange_jobs","id":"%s","tenant_id":"%s","project_id":"%s","created_at":"2026-10-13T10:00:00Z","updated_at":"2026-10-13T10:00:00Z","revision":"1","workflow_id":"%s","direction":"import","format":"photoeditor_bundle","mapping_artifact_id":null,"report_artifact_id":null,"bundle_artifact_id":null}}`, job, job, tid, project, job)
	tx := &jobReadTx{raw: []byte(valid)}
	got, err := ReadJob(context.Background(), tx, who, tid, job)
	if err != nil || len(tx.args) != 4 || tx.args[0] != tid || tx.args[1] != who.UserID || tx.args[2] != who.SessionID || tx.args[3] != job {
		t.Fatal("read scope changed", err)
	}
	tx.raw[0] = 'x'
	if string(got) != valid {
		t.Fatal("returned borrowed bytes")
	}
	for _, raw := range []string{"null", "", "invalid", "[]", "true", fmt.Sprintf(`{"id":"%s"}`, job), fmt.Sprintf(`{"id":"%s","revision":"1","document":null,"data":{"resource_type":"interchange_jobs","id":"%s","tenant_id":"%s","project_id":"%s","created_at":"2026-10-13T10:00:00Z","updated_at":"2026-10-13T10:00:00Z","revision":"1","workflow_id":"%s","direction":"import","format":"xmp","mapping_artifact_id":null,"report_artifact_id":null,"bundle_artifact_id":null}}`, job, job, uuid.New(), project, job)} {
		tx.raw = []byte(raw)
		if _, err := ReadJob(context.Background(), tx, who, tid, job); err == nil {
			t.Fatal("missing/foreign SQL resource accepted")
		}
	}
}

func TestReadInterchangeRejectsAmbiguousDocumentReference(t *testing.T) {
	tid, who, _, _, _ := pendingFixture()
	job, project, artifact := uuid.New(), uuid.New(), uuid.New()
	base := fmt.Sprintf(`{"id":%q,"revision":"1","document":{"artifact_id":%q,"sha256":%q},"data":{"resource_type":"interchange_jobs","id":%q,"tenant_id":%q,"project_id":%q,"created_at":"2026-10-13T10:00:00Z","updated_at":"2026-10-13T10:00:00Z","revision":"1","workflow_id":%q,"direction":"import","format":"photoeditor_bundle","mapping_artifact_id":null,"report_artifact_id":null,"bundle_artifact_id":null}}`, job.String(), artifact.String(), strings.Repeat("a", 64), job.String(), tid.String(), project.String(), job.String())
	if _, err := ReadJob(context.Background(), &jobReadTx{raw: []byte(base)}, who, tid, job); err != nil {
		t.Fatal("valid document reference rejected", err)
	}
	for _, altered := range []string{
		strings.Replace(base, `"document":{"artifact_id":`, `"document":{"artifact_id":`+fmt.Sprintf("%q,", artifact.String())+`"artifact_id":`, 1),
		strings.Replace(base, `"document":{"artifact_id":`, `"document":{"object_key":"secret","artifact_id":`, 1),
	} {
		if altered == base {
			t.Fatal("fixture replacement failed")
		}
		if _, err := ReadJob(context.Background(), &jobReadTx{raw: []byte(altered)}, who, tid, job); err == nil {
			t.Fatal("ambiguous document reference accepted", altered)
		}
	}
}

func TestReadInterchangeDropsCancelledSQLResponse(t *testing.T) {
	tid, who, _, _, _ := pendingFixture()
	job, project, run := uuid.New(), uuid.New(), uuid.New()
	resource := fmt.Sprintf(`{"id":%q,"revision":"1","document":null,"data":{"resource_type":"interchange_jobs","id":%q,"tenant_id":%q,"project_id":%q,"created_at":"2026-10-13T10:00:00Z","updated_at":"2026-10-13T10:00:00Z","revision":"1","workflow_id":%q,"direction":"import","format":"photoeditor_bundle","mapping_artifact_id":null,"report_artifact_id":null,"bundle_artifact_id":null}}`, job.String(), job.String(), tid.String(), project.String(), job.String())
	inspection := fmt.Sprintf(`{"schema_version":1,"workflow_id":%q,"tenant_id":%q,"project_id":%q,"run_id":%q,"revision":"1","status":"running","stage":"parsing","ingest_run_id":null,"collection_id":null,"review":null}`, job.String(), tid.String(), project.String(), run.String())
	for _, tc := range []struct {
		raw     string
		inspect bool
	}{{resource, false}, {inspection, true}} {
		ctx, cancel := context.WithCancel(context.Background())
		tx := &jobReadTx{raw: []byte(tc.raw), cancel: cancel}
		got, err := readJobProjection(ctx, tx, who, tid, job, tc.inspect)
		if !errors.Is(err, context.Canceled) || len(got) != 0 {
			t.Fatal("cancelled SQL response escaped", tc.inspect, err)
		}
	}
}

func TestInspectInterchangeRejectsForeignOrMalformedProjection(t *testing.T) {
	tid, who, _, _, _ := pendingFixture()
	job := uuid.New()
	project := uuid.New()
	run := uuid.New()
	valid := fmt.Sprintf(`{"schema_version":1,"workflow_id":"%s","tenant_id":"%s","project_id":"%s","run_id":"%s","revision":"4","status":"running","stage":"parsing","ingest_run_id":null,"collection_id":null,"review":null}`, job, tid, project, run)
	tx := &jobReadTx{raw: []byte(valid)}
	got, err := readJobProjection(context.Background(), tx, who, tid, job, true)
	if err != nil || string(got) != valid || len(tx.args) != 4 || tx.args[0] != tid || tx.args[3] != job {
		t.Fatal("valid owned inspection rejected", err)
	}
	tx.raw[0] = 'x'
	if string(got) != valid {
		t.Fatal("inspection returned borrowed bytes")
	}
	review := fmt.Sprintf(`{"schema_version":1,"workflow_id":"%s","tenant_id":"%s","project_id":"%s","run_id":"%s","revision":"5","status":"waiting_review","stage":"review","ingest_run_id":null,"collection_id":null,"review":{"report":{"artifact_id":"%s","sha256":"%064x"},"proposed_count":0,"entry_count":1,"can_close":true,"can_decide":true,"targets":[]}}`, job, tid, project, run, uuid.New(), 1)
	tx.raw = []byte(review)
	if _, err := readJobProjection(context.Background(), tx, who, tid, job, true); err != nil {
		t.Fatal("valid review inspection rejected", err)
	}
	target := fmt.Sprintf(`{"schema_version":1,"workflow_id":"%s","tenant_id":"%s","project_id":"%s","run_id":"%s","revision":"5","status":"waiting_review","stage":"review","ingest_run_id":null,"collection_id":null,"review":{"report":{"artifact_id":"%s","sha256":"%064x"},"proposed_count":1,"entry_count":1,"can_close":false,"can_decide":true,"targets":[{"entry_id":"%s","external_id":"capture-1","asset_version_id":"%s","state":"proposed","fields":[{"name":"exposure","status":"exact","reason":null}]}]}}`, job, tid, project, run, uuid.New(), 1, uuid.New(), uuid.New())
	tx.raw = []byte(target)
	if _, err := readJobProjection(context.Background(), tx, who, tid, job, true); err != nil {
		t.Fatal("valid review target rejected", err)
	}
	malformedTarget := []byte(strings.Replace(target, `"state":"proposed"`, `"state":"approved"`, 1))
	if _, err := readJobProjection(context.Background(), &jobReadTx{raw: malformedTarget}, who, tid, job, true); err == nil {
		t.Fatal("invalid review target state accepted")
	}
	for _, raw := range []string{strings.Replace(review, `"targets":[]`, `"targets":null`, 1), strings.Replace(target, `"proposed_count":1`, `"proposed_count":null`, 1), strings.Replace(target, `"can_decide":true`, `"can_decide":null`, 1), strings.Replace(target, `"fields":[{"name":"exposure","status":"exact","reason":null}]`, `"fields":null`, 1)} {
		if _, err := readJobProjection(context.Background(), &jobReadTx{raw: []byte(raw)}, who, tid, job, true); err == nil {
			t.Fatal("invalid review field type accepted")
		}
	}
	for _, raw := range []string{"[]", "true", fmt.Sprintf(`{"schema_version":1,"workflow_id":"%s","tenant_id":"%s","project_id":"%s","run_id":"%s","revision":"4","status":"running","stage":"parsing","ingest_run_id":null,"collection_id":null,"review":null}`, job, uuid.New(), project, run), fmt.Sprintf(`{"schema_version":1,"workflow_id":"%s","tenant_id":"%s","project_id":"%s","run_id":"%s","revision":"4","status":"completed","stage":"parsing","ingest_run_id":null,"collection_id":null,"review":null}`, job, tid, project, run), fmt.Sprintf(`{"schema_version":1,"workflow_id":"%s","tenant_id":"%s","project_id":"%s","run_id":"%s","revision":"4","status":"running","stage":"parsing","ingest_run_id":null,"collection_id":null}`, job, tid, project, run)} {
		tx.raw = []byte(raw)
		if _, err := readJobProjection(context.Background(), tx, who, tid, job, true); err == nil {
			t.Fatal("malformed or foreign inspection accepted", raw)
		}
	}
}
