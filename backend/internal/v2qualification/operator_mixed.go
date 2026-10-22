package v2qualification

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/canonical"
)

// OperatorMixedChildren is measurement authority only. It has no parent ID,
// cannot serialize into client run config, and never authorizes SQL task admission.
// Its private fields can only be populated after current child/operator readback.
type OperatorMixedChildren struct {
	tenant, run  uuid.UUID
	corpus       string
	dependencies MixedAuthority
}

func ResolveOperatorMixedChildren(ctx context.Context, db interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, tenant, project, actor, run uuid.UUID, expected MixedExpected) (*OperatorMixedChildren, error) {
	if db == nil || tenant == uuid.Nil || project == uuid.Nil || actor == uuid.Nil || run == uuid.Nil || !digest(expected.Common.DatasetSHA) {
		return nil, ErrUnqualified
	}
	children, err := canonical.Marshal(expected.Children)
	if err != nil {
		return nil, err
	}
	routes, err := canonical.Marshal(expected.Routes)
	if err != nil {
		return nil, err
	}
	var ok bool
	if err = db.QueryRow(ctx, `SELECT photo.authorize_operator_mixed_children($1,$2,$3,$4::jsonb,$5::jsonb,$6)`, tenant, project, actor, string(children), string(routes), expected.Common.DatasetSHA).Scan(&ok); err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrUnqualified
	}
	// Canonical roundtrip owns maps/slices independently from caller memory.
	var owned []MixedChild
	var ownedRoutes map[string]string
	if json.Unmarshal(children, &owned) != nil || json.Unmarshal(routes, &ownedRoutes) != nil {
		return nil, ErrUnqualified
	}
	sum := sha256.Sum256(children)
	a := MixedAuthority{Children: owned, Routes: ownedRoutes, DependenciesSHA: hex.EncodeToString(sum[:])}
	if a.validateDependencies() != nil {
		return nil, ErrUnqualified
	}
	return &OperatorMixedChildren{tenant: tenant, run: run, corpus: expected.Common.DatasetSHA, dependencies: a}, nil
}
func (o *OperatorMixedChildren) QualifyProfile(tenant, run uuid.UUID, corpus, operation string, profile json.RawMessage) (json.RawMessage, error) {
	if o == nil || tenant != o.tenant || run != o.run || corpus != o.corpus || !digest(corpus) {
		return nil, ErrUnqualified
	}
	return o.dependencies.QualifyProfile(operation, "1", profile)
}
