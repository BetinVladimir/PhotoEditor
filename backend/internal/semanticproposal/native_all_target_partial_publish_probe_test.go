package semanticproposal

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/v2input"
)

var errNativePartialPublishProbe = errors.New("injected native transport publication interruption")

type nativePartialPublishProbe struct {
	publisher v2input.Publisher
	calls     int
	lostAck   bool
}

func (p *nativePartialPublishProbe) Publish(ctx context.Context, task uuid.UUID, ordinal int, contract string, body []byte) (v2input.Ref, error) {
	p.calls++
	if p.calls == 2 {
		if p.lostAck {
			if _, err := p.publisher.Publish(ctx, task, ordinal, contract, body); err != nil {
				return v2input.Ref{}, err
			}
		}
		return v2input.Ref{}, errNativePartialPublishProbe
	}
	return p.publisher.Publish(ctx, task, ordinal, contract, body)
}

type nativePartialPublicationDocument struct {
	artifact     uuid.UUID
	sha, version string
}
type nativePartialPublicationObservation struct {
	documents []nativePartialPublicationDocument
	reserved  int64
}

func observeNativePartialPublication(t *testing.T, ctx context.Context, owner *pgxpool.Pool, original detectorDraftPlan) nativePartialPublicationObservation {
	t.Helper()
	var out nativePartialPublicationObservation
	var sealed bool
	var missing, documents int
	if err := owner.QueryRow(ctx, "SELECT graph_sealed FROM photo.runs WHERE tenant_id=$1 AND id=$2", original.request.TenantID, original.request.RunID).Scan(&sealed); err != nil || sealed {
		t.Fatal("partial graph must remain blocked", sealed, err)
	}
	if err := owner.QueryRow(ctx, "SELECT count(*) FROM photo.tasks WHERE tenant_id=$1 AND run_id=$2 AND state='blocked' AND NOT(execution_profile ? 'v2_input_manifest')", original.request.TenantID, original.request.RunID).Scan(&missing); err != nil || missing < 1 {
		t.Fatal("missing registration fixture", missing, err)
	}
	expectedDocuments := 1
	if nativePartialLostAckEnabled() {
		expectedDocuments = 2
	}
	if err := owner.QueryRow(ctx, "SELECT count(*) FROM photo.backend_documents WHERE tenant_id=$1 AND run_id=$2 AND purpose LIKE 'v2_input_%' AND state='ready'", original.request.TenantID, original.request.RunID).Scan(&documents); err != nil || documents != expectedDocuments {
		t.Fatal("unexpected durable document count", documents, expectedDocuments, err)
	}
	rows, err := owner.Query(ctx, "SELECT artifact_id,sha256,object_version FROM photo.backend_documents WHERE tenant_id=$1 AND run_id=$2 AND purpose LIKE 'v2_input_%' AND state='ready' ORDER BY artifact_id", original.request.TenantID, original.request.RunID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var doc nativePartialPublicationDocument
		if err := rows.Scan(&doc.artifact, &doc.sha, &doc.version); err != nil {
			t.Fatal(err)
		}
		out.documents = append(out.documents, doc)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if err := owner.QueryRow(ctx, "SELECT bytes FROM photo.quota_reservations WHERE tenant_id=$1 AND holder_type='run' AND holder_id=$2 AND state='reserved'", original.request.TenantID, original.request.RunID).Scan(&out.reserved); err != nil {
		t.Fatal(err)
	}
	return out
}
func assertNativePartialPublicationRecovered(t *testing.T, ctx context.Context, owner *pgxpool.Pool, original detectorDraftPlan, before nativePartialPublicationObservation) {
	t.Helper()
	var sha, version string
	var reserved int64
	for _, doc := range before.documents {
		if err := owner.QueryRow(ctx, "SELECT sha256,object_version FROM photo.backend_documents WHERE tenant_id=$1 AND run_id=$2 AND artifact_id=$3 AND state='ready'", original.request.TenantID, original.request.RunID, doc.artifact).Scan(&sha, &version); err != nil || sha != doc.sha || version != doc.version {
			t.Fatal("published document replaced", err)
		}
	}
	if err := owner.QueryRow(ctx, "SELECT bytes FROM photo.quota_reservations WHERE tenant_id=$1 AND holder_type='run' AND holder_id=$2", original.request.TenantID, original.request.RunID).Scan(&reserved); err != nil || reserved != before.reserved {
		t.Fatal("run reservation changed", reserved, before.reserved, err)
	}
	t.Log("V27_NATIVE_ALL_TARGET_PARTIAL_PUBLICATION_RECOVERED_OK")
	if nativePartialLostAckEnabled() {
		t.Log("V27_NATIVE_ALL_TARGET_LOST_PUBLICATION_ACK_RECOVERED_OK")
	}
}
func nativePartialPublicationEnabled() bool {
	return os.Getenv("PHOTO_TEST_V27_NATIVE_ALL_TARGET_PARTIAL_JOB") == "1"
}

func nativePartialLostAckEnabled() bool {
	return os.Getenv("PHOTO_TEST_V27_NATIVE_ALL_TARGET_LOST_ACK") == "1"
}
