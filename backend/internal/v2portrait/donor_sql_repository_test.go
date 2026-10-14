package v2portrait

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

type donorSQLRow func(...any) error

func (r donorSQLRow) Scan(dest ...any) error { return r(dest...) }

type donorSQLTx struct {
	proof []byte
	donorIssueTransaction
	mode      string
	id, token uuid.UUID
	queries   int
	args      []any
}

func (tx *donorSQLTx) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	tx.queries++
	tx.args = args
	return donorSQLRow(func(dest ...any) error {
		if query == "SELECT current_user" {
			role := "photo_scheduler"
			if tx.mode == "role" {
				role = "photo_api"
			}
			*dest[0].(*string) = role
			return nil
		}
		if tx.mode == "query_error" {
			return errors.New("query failed")
		}
		if strings.Contains(query, "build_v28_donor_confirmation") {
			*dest[0].(*[]byte) = append([]byte(nil), tx.proof...)
			return nil
		}
		if strings.Contains(query, "reserve_v28") {
			outcome := tx.mode
			switch outcome {
			case "role", "commit_error", "query_error", "nil_id", "ready_mismatch":
				outcome = "created"
			}
			*dest[0].(*string) = outcome
			if tx.mode != "nil_id" {
				*dest[1].(**uuid.UUID) = &tx.id
			}
			*dest[2].(**uuid.UUID) = &tx.token
			*dest[3].(*bool) = outcome == "replayed" || tx.mode == "ready_mismatch"
		} else {
			*dest[0].(*bool) = tx.mode != "denied"
		}
		return nil
	})
}

type donorSQLDB struct {
	tx     *donorSQLTx
	begins int
}

func (db *donorSQLDB) Begin(context.Context) (pgx.Tx, error) { db.begins++; return db.tx, nil }

func checkDonorSQLRepository(t *testing.T, request DonorConfirmationPublication) {
	t.Helper()
	for _, mode := range []string{"created", "resumed", "replayed", "conflict", "stale", "forbidden", "quota", "unknown", "role", "nil_id", "ready_mismatch", "query_error", "commit_error"} {
		tx := &donorSQLTx{mode: mode, id: uuid.New(), token: uuid.New()}
		tx.failCommit = mode == "commit_error"
		db := &donorSQLDB{tx: tx}
		repo := SQLDonorConfirmationRepository{DB: db, Bucket: "photos"}
		result, err := repo.ReserveDonorConfirmation(context.Background(), request, int64(len(request.Prepared.Body)))
		success := mode == "created" || mode == "resumed" || mode == "replayed"
		if success {
			if err != nil || result.RunID != request.RunID || result.ArtifactID != tx.id || result.Token != tx.token || (result.Ready != nil) != (mode == "replayed") || tx.commits != 1 {
				t.Fatal("reserve", mode, result, err)
			}
			if len(tx.args) != 7 || tx.args[0] != request.Tenant || tx.args[1] != request.RunID || tx.args[2] != request.Prepared.Params.Confirmation.ArtifactID {
				t.Fatal("reserve scope lost")
			}
		} else if err == nil || result.ArtifactID != uuid.Nil || result.Ready != nil {
			t.Fatal("failed reserve leaked ID", mode, err)
		}
		if tx.rollbacks != 1 || tx.tenantArg != request.Tenant.String() {
			t.Fatal("reserve cleanup/scope", mode)
		}
		if mode == "role" && tx.queries != 1 {
			t.Fatal("wrong role reached reserve")
		}
	}
	for _, mode := range []string{"ok", "denied", "role", "query_error", "commit_error", "wrong_object"} {
		tx := &donorSQLTx{mode: mode, id: uuid.New(), token: uuid.New()}
		tx.failCommit = mode == "commit_error"
		db := &donorSQLDB{tx: tx}
		repo := SQLDonorConfirmationRepository{DB: db, Bucket: "photos"}
		reservation := DonorConfirmationReservation{RunID: request.RunID, ArtifactID: tx.id, Token: tx.token}
		key, _ := objectstore.DocumentKey(request.Tenant, request.RunID, tx.id)
		object := objectstore.Object{Key: key, SHA256: request.Prepared.SHA256, Bytes: int64(len(request.Prepared.Body)), ContentType: "application/json", VersionID: "pinned"}
		if mode == "wrong_object" {
			object.VersionID = "null"
		}
		ref, err := repo.CompleteDonorConfirmation(context.Background(), request, reservation, object)
		if mode == "ok" {
			if err != nil || ref.ArtifactID != tx.id || ref.SHA256 != request.Prepared.SHA256 || tx.commits != 1 || len(tx.args) != 10 {
				t.Fatal("complete", ref, err)
			}
		} else if err == nil || ref != (v2input.Ref{}) {
			t.Fatal("failed completion leaked ref", mode, err)
		}
		if mode == "wrong_object" && db.begins != 0 {
			t.Fatal("invalid object reached DB")
		}
		if mode != "wrong_object" && tx.rollbacks != 1 {
			t.Fatal("completion cleanup")
		}
	}
	tx := &donorSQLTx{mode: "created", id: uuid.New(), token: uuid.New(), proof: request.Prepared.Body}
	db := &donorSQLDB{tx: tx}
	store := &donorObjectStore{}
	service := OpenEyesConfirmationService{DB: db, Objects: store, Bucket: "photos"}
	bound, err := service.Issue(context.Background(), request.Actor, request.Tenant, request.Project, request.ShootSession, request.RunID, request.Prepared.Params, request.SourceObservation, request.DonorObservation)
	if err != nil || bound.Confirmation.ArtifactID != tx.id || bound.Confirmation.SHA256 != request.Prepared.SHA256 || db.begins != 4 || tx.commits != 4 || store.puts != 1 {
		t.Fatal("composed issuance", err, db.begins, tx.commits, store.puts)
	}

}
