// Operator-only closed intake scope. Does not add SQL or weaken production authority.
package mediascope

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	retryscope "photoeditor/backend/_work/user-raw-retry-scope-r3"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/workflow"
	"strings"
)

const ClaimQuery = "SELECT event_id,materialize_token,tenant_id,bulk_id,item_id,asset_version_id,variants,master_artifact_id,bucket,object_key,object_version,sha256,bytes,media_type,width,height,output_role FROM photo.claim_media_retry_job()"

var ErrOutsideScope = errors.New("media claim outside reviewed closed scope")

type Binding struct {
	EventID   uuid.UUID `json:"event_id"`
	TenantID  uuid.UUID `json:"tenant_id"`
	VersionID uuid.UUID `json:"version_id"`
}
type Scope map[uuid.UUID]Binding

func NewScope(bindings []Binding) (Scope, error) {
	if len(bindings) != 17 {
		return nil, errors.New("exact seventeen media events required")
	}
	scope := Scope{}
	for _, b := range bindings {
		if b.EventID == uuid.Nil || b.TenantID == uuid.Nil || b.VersionID == uuid.Nil {
			return nil, ErrOutsideScope
		}
		if _, exists := scope[b.EventID]; exists {
			return nil, ErrOutsideScope
		}
		scope[b.EventID] = b
	}
	return scope, nil
}
func (s Scope) Allows(event, tenant, version uuid.UUID) bool {
	b, ok := s[event]
	return ok && b.TenantID == tenant && b.VersionID == version
}
func (s Scope) AllowsLease(l *workflow.MediaRetryLease) bool {
	return l != nil && s.Allows(l.EventID, l.TenantID, l.AssetVersionID)
}

// GuardedClaims validates the SQL claim row BEFORE MediaRetryStore can read a
// working-image object or call its policy resolver. Foreign SQL mutation remains
// inside CheckedClaim's transaction and is rolled back, never released/committed.
type GuardedClaims struct {
	DB    artifactjson.RowQuerier
	Scope Scope
}

func (g GuardedClaims) QueryRow(ctx context.Context, q string, args ...any) pgx.Row {
	if q == ClaimQuery {
		return guardedRow{row: g.DB.QueryRow(ctx, q, args...), scope: g.Scope}
	}
	if strings.Contains(strings.ToLower(q), "claim_media_retry_job") {
		return rejectedRow{}
	}
	return g.DB.QueryRow(ctx, q, args...)
}

type guardedRow struct {
	row   pgx.Row
	scope Scope
}

func (r guardedRow) Scan(dest ...any) error {
	if len(dest) != 17 {
		return ErrOutsideScope
	}
	if err := r.row.Scan(dest...); err != nil {
		return err
	}
	event, eok := dest[0].(*uuid.UUID)
	tenant, tok := dest[2].(*uuid.UUID)
	version, vok := dest[5].(*uuid.UUID)
	if !eok || !tok || !vok || event == nil || tenant == nil || version == nil || !r.scope.Allows(*event, *tenant, *version) {
		return ErrOutsideScope
	}
	return nil
}

type Store struct {
	workflow.MediaRetryStore
	BeginClaimTx func(context.Context) (retryscope.Transaction, error)
	Scope        Scope
}

func (s Store) Claim(ctx context.Context) (*workflow.MediaRetryLease, error) {
	return retryscope.CheckedClaim(ctx, s.BeginClaimTx, func(tx retryscope.Transaction) (*workflow.MediaRetryLease, error) {
		actual, ok := tx.(artifactjson.RowQuerier)
		if !ok {
			return nil, errors.New("claim transaction cannot query")
		}
		store := s.MediaRetryStore
		store.DB = GuardedClaims{DB: actual, Scope: s.Scope}
		return store.Claim(ctx)
	}, s.Scope.AllowsLease)
}

type rejectedRow struct{}

func (rejectedRow) Scan(...any) error { return ErrOutsideScope }
