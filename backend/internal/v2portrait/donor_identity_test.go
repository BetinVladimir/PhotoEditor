package v2portrait

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

type donorIdentityRow struct {
	role  string
	ready bool
	raw   []byte
}

func (r donorIdentityRow) Scan(dest ...any) error {
	switch value := dest[0].(type) {
	case *string:
		*value = r.role
	case *bool:
		*value = r.ready
	case *[]byte:
		*value = append([]byte(nil), r.raw...)
	}
	return nil
}

type donorIdentityTx struct {
	pgx.Tx
	role  string
	ready bool
	calls int
	args  []any
	raw   []byte
}

func (tx *donorIdentityTx) QueryRow(_ context.Context, _ string, args ...any) pgx.Row {
	tx.calls++
	tx.args = args
	return donorIdentityRow{role: tx.role, ready: tx.ready, raw: tx.raw}
}

type donorIssueTransaction struct {
	donorIdentityTx
	commits, rollbacks, execs int
	failCommit                bool
	tenantArg                 string
}

func (tx *donorIssueTransaction) Exec(_ context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
	tx.execs++
	tx.tenantArg = args[0].(string)
	return pgconn.CommandTag{}, nil
}
func (tx *donorIssueTransaction) Commit(context.Context) error {
	tx.commits++
	if tx.failCommit {
		return errors.New("commit failed")
	}
	return nil
}
func (tx *donorIssueTransaction) Rollback(context.Context) error { tx.rollbacks++; return nil }

type donorIssueDB struct {
	tx     *donorIssueTransaction
	begins int
}

func (db *donorIssueDB) Begin(context.Context) (pgx.Tx, error) { db.begins++; return db.tx, nil }

type donorIssuePublisher struct {
	tx          *donorIssueTransaction
	mode        string
	calls       int
	expectedSHA string
	alias       uuid.UUID
}

func (p *donorIssuePublisher) PublishDonorConfirmation(_ context.Context, request DonorConfirmationPublication) (v2input.Ref, error) {
	p.calls++
	if p.tx.commits != 1 {
		return v2input.Ref{}, errors.New("publisher called before commit")
	}
	if request.Prepared.SHA256 != p.expectedSHA || request.Actor.UserID == uuid.Nil || request.Tenant == uuid.Nil || request.Project == uuid.Nil || request.ShootSession == uuid.Nil || request.RunID == uuid.Nil {
		return v2input.Ref{}, errors.New("issuance scope lost")
	}
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: p.expectedSHA}
	switch p.mode {
	case "mutate":
		request.Prepared.Body[0] ^= 1
	case "wrong_sha":
		ref.SHA256 = strings.Repeat("f", 64)
	case "alias":
		ref.ArtifactID = p.alias
	case "revoked":
		p.tx.raw = nil
	case "error":
		return v2input.Ref{}, errors.New("publication failed")
	}
	return ref, nil
}

func TestLockDonorIdentityRequiresCallerAndScheduler(t *testing.T) {
	binding := func() v2input.SessionVersionBinding {
		return v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}, CanvasHash: strings.Repeat("b", 64)}
	}
	donor := binding()
	face, observation := uuid.New(), uuid.New()
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	proof := DonorConfirmation{SchemaVersion: 1, PersonID: uuid.New(), Source: binding(), Donor: &donor, SourceFaceID: uuid.New(), DonorFaceID: &face, SourceObservationID: uuid.New(), DonorObservationID: &observation, ConsentID: uuid.New(), Epoch: "1", ActorID: who.UserID}
	tenant, project := uuid.New(), uuid.New()
	ref := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("c", 64)} }
	params := OpenEyesParams{SchemaVersion: 1, Base: proof.Source, Donor: donor, Strength: 1, SourceFaceID: proof.SourceFaceID, DonorFaceID: face, EyesMask: ref(), Protected: ref(), Confirmation: ref(), SourceFaces: ref(), DonorFaces: ref()}
	proofRaw, proofSHA, err := proof.Compile()
	if err != nil {
		t.Fatal(err)
	}
	params.Confirmation.SHA256 = proofSHA
	checkDonorFaceEvidence(t, params, proofRaw)
	aliased := params
	aliased.SourceFaces = aliased.Confirmation
	txAlias := &donorIssueTransaction{donorIdentityTx: donorIdentityTx{role: "photo_scheduler", raw: proofRaw}}
	dbAlias := &donorIssueDB{tx: txAlias}
	if prepared, err := PrepareOpenEyesConfirmation(context.Background(), dbAlias, who, tenant, project, uuid.New(), aliased, proof.SourceObservationID, *proof.DonorObservationID); err == nil || prepared.Body != nil || dbAlias.begins != 0 {
		t.Fatal("JSON root alias reached DB")
	}
	if body, _, err := BuildOpenEyesConfirmation(context.Background(), txAlias, who, tenant, project, uuid.New(), aliased, proof.SourceObservationID, *proof.DonorObservationID); err == nil || body != nil || txAlias.calls != 0 {
		t.Fatal("alias reached issuer SQL")
	}
	shoot := uuid.New()
	checkDonorObjectPublisher(t, DonorConfirmationPublication{Actor: who, Tenant: tenant, Project: project, ShootSession: shoot, RunID: uuid.New(), SourceObservation: proof.SourceObservationID, DonorObservation: *proof.DonorObservationID, Prepared: PreparedDonorConfirmation{Body: proofRaw, SHA256: proofSHA, Params: params}})

	for _, mode := range []string{"ok", "wrong_role", "missing", "wrong_actor", "wrong_source", "wrong_observation", "unknown_field", "formatted"} {
		tx := &donorIdentityTx{role: "photo_scheduler", raw: proofRaw}
		if mode == "wrong_role" {
			tx.role = "photo_api"
		}
		if mode == "missing" {
			tx.raw = nil
		}
		if mode == "wrong_actor" {
			changed := proof
			changed.ActorID = uuid.New()
			tx.raw, _, _ = changed.Compile()
		}
		switch mode {
		case "wrong_source":
			changed := proof
			changed.Source.AssetVersionID = uuid.New()
			tx.raw, _, _ = changed.Compile()
		case "wrong_observation":
			changed := proof
			changed.SourceObservationID = uuid.New()
			tx.raw, _, _ = changed.Compile()
		case "unknown_field":
			var fields map[string]any
			json.Unmarshal(proofRaw, &fields)
			fields["authority_override"] = true
			tx.raw, _ = json.Marshal(fields)
		case "formatted":
			var fields map[string]any
			json.Unmarshal(proofRaw, &fields)
			tx.raw, _ = json.MarshalIndent(fields, "", "  ")
		}
		body, digest, err := BuildOpenEyesConfirmation(context.Background(), tx, who, tenant, project, shoot, params, proof.SourceObservationID, *proof.DonorObservationID)
		if mode == "ok" || mode == "formatted" {
			if err != nil || digest != proofSHA || string(body) != string(proofRaw) || tx.calls != 2 {
				t.Fatal("issued proof", err, tx.calls)
			}
		} else if err == nil || body != nil || digest != "" {
			t.Fatal("untrusted issuer result", mode, err)
		}
		if mode == "wrong_role" && tx.calls != 1 {
			t.Fatal("wrong role reached issuer")
		}
	}

	for _, mode := range []string{"ok", "commit_failure", "missing_proof"} {
		tx := &donorIssueTransaction{donorIdentityTx: donorIdentityTx{role: "photo_scheduler", raw: proofRaw}, failCommit: mode == "commit_failure"}
		if mode == "missing_proof" {
			tx.raw = nil
		}
		db := &donorIssueDB{tx: tx}
		prepared, err := PrepareOpenEyesConfirmation(context.Background(), db, who, tenant, project, shoot, params, proof.SourceObservationID, *proof.DonorObservationID)
		if tx.execs != 1 || tx.tenantArg != tenant.String() || tx.rollbacks != 1 {
			t.Fatal("transaction scope/cleanup", mode, tx)
		}
		if mode == "ok" {
			if err != nil || prepared.SHA256 != proofSHA || string(prepared.Body) != string(proofRaw) || prepared.Params.Confirmation.SHA256 != proofSHA || tx.commits != 1 {
				t.Fatal("committed preparation", err)
			}
		} else if err == nil || prepared.Body != nil || prepared.SHA256 != "" {
			t.Fatal("failed transaction leaked prepared bytes", mode, err)
		}
		if mode == "missing_proof" && tx.commits != 0 {
			t.Fatal("invalid proof committed")
		}
	}
	for _, mode := range []string{"ok", "mutate", "wrong_sha", "alias", "revoked", "error"} {
		tx := &donorIssueTransaction{donorIdentityTx: donorIdentityTx{role: "photo_scheduler", raw: proofRaw}}
		db := &donorIssueDB{tx: tx}
		publisher := &donorIssuePublisher{tx: tx, mode: mode, expectedSHA: proofSHA, alias: params.Base.Source.ArtifactID}
		bound, err := IssueOpenEyesConfirmation(context.Background(), db, publisher, who, tenant, project, shoot, uuid.New(), params, proof.SourceObservationID, *proof.DonorObservationID)
		if mode == "ok" || mode == "mutate" {
			if err != nil || bound.Confirmation.SHA256 != proofSHA || bound.Confirmation.ArtifactID == uuid.Nil || tx.commits != 2 {
				t.Fatal("published issuance", mode, err, tx.commits)
			}
		} else if err == nil || bound != (OpenEyesParams{}) {
			t.Fatal("failed publication returned authority", mode, err)
		}
		if publisher.calls != 1 {
			t.Fatal("publication count")
		}
	}
	sessionTx := &donorIdentityTx{role: "photo_scheduler", ready: true}
	if err = LockOpenEyesDonorSession(context.Background(), sessionTx, who, tenant, project, shoot, params, proofRaw); err != nil || sessionTx.calls != 2 || len(sessionTx.args) != 8 || sessionTx.args[2] != shoot {
		t.Fatal("donor session binding", err)
	}
	sourceJSON, _ := canonical.Marshal(params.SourceFaces)
	donorJSON, _ := canonical.Marshal(params.DonorFaces)
	if sessionTx.args[6] != string(sourceJSON) || sessionTx.args[7] != string(donorJSON) {
		t.Fatal("face evidence refs lost")
	}
	denied := &donorIdentityTx{role: "photo_scheduler", ready: false}
	if err = LockOpenEyesDonorSession(context.Background(), denied, who, tenant, project, shoot, params, proofRaw); err != v2input.ErrExpired {
		t.Fatal("stale session not denied", err)
	}
	changed := params
	changed.DonorFaceID = uuid.New()
	preflight := &donorIdentityTx{role: "photo_scheduler", ready: true}
	if err = LockOpenEyesDonorSession(context.Background(), preflight, who, tenant, project, shoot, changed, proofRaw); err == nil || preflight.calls != 0 {
		t.Fatal("changed face reached session SQL")
	}

	for _, mode := range []string{"valid", "wrong_role", "revoked", "wrong_actor", "cancelled"} {
		tx := &donorIdentityTx{role: "photo_scheduler", ready: true}
		caller := who
		ctx := context.Background()
		switch mode {
		case "wrong_role":
			tx.role = "photo_api"
		case "revoked":
			tx.ready = false
		case "wrong_actor":
			caller.UserID = uuid.New()
		case "cancelled":
			c, cancel := context.WithCancel(ctx)
			cancel()
			ctx = c
		}
		err := LockDonorIdentity(ctx, tx, caller, tenant, project, proof)
		if mode == "valid" {
			if err != nil || tx.calls != 2 || len(tx.args) != 9 || tx.args[0] != tenant || tx.args[2] != proof.PersonID || tx.args[8] != who.SessionID {
				t.Fatal("wrong identity gate binding", err)
			}
		} else if err == nil {
			t.Fatal("invalid identity gate accepted", mode)
		}
		if (mode == "wrong_actor" || mode == "cancelled") && tx.calls != 0 {
			t.Fatal("invalid caller reached SQL")
		}
		if mode == "wrong_role" && tx.calls != 1 {
			t.Fatal("wrong role reached identity function")
		}
	}
}
