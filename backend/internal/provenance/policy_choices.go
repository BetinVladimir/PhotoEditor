package provenance

import (
	"bytes"
	"context"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/domain"
)

// Redacted UI projection; presence is availability, not admission authority.
type SigningPolicyChoice struct {
	ID       uuid.UUID `json:"id"`
	Revision string    `json:"revision"`
}

// Advisory Delivery choice: the SHA pins the exact installed policy, while
// creation and each signed child repeat live qualification independently.
type DeliverySigningPolicyChoice struct {
	ID       uuid.UUID `json:"id"`
	SHA256   string    `json:"sha256"`
	Revision string    `json:"revision"`
}

type signingRuntimePolicyReader interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// Advisory listing follows the same exact policy ID/SHA lease as admission.
// The final SignExport transaction still repeats the live SQL gate.
func filterSigningChoicesByAdopter(ctx context.Context, db signingRuntimePolicyReader, bucket string, registry *SigningPolicyRegistry, choices []SigningPolicyChoice) ([]SigningPolicyChoice, error) {
	if db == nil || registry == nil || len(bucket) < 3 || len(bucket) > 63 {
		return nil, ErrInvalid
	}
	visible := make([]SigningPolicyChoice, 0, len(choices))
	for _, choice := range choices {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		installed, ok := registry.policies[choice.ID]
		if !ok || installed.ref.SHA256 == "" {
			return nil, ErrInvalid
		}
		params, err := canonical.Marshal(map[string]any{
			"signing_policy_id": choice.ID.String(),
			"policy":            map[string]string{"sha256": installed.ref.SHA256},
		})
		if err != nil {
			return nil, err
		}
		var ready bool
		if err := db.QueryRow(ctx, `SELECT photo.v31_signing_adopter_runtime_ready($1,$2)`, bucket, string(params)).Scan(&ready); err != nil {
			return nil, err
		}
		if ready {
			visible = append(visible, choice)
		}
	}
	return visible, ctx.Err()
}

func (r *SigningPolicyRegistry) availableDelivery(ctx context.Context, mimes []string, now time.Time) ([]DeliverySigningPolicyChoice, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r == nil || r.authority == nil || now.IsZero() || len(mimes) < 1 || len(mimes) > 2 {
		return nil, ErrInvalid
	}
	wanted := make(map[string]bool, len(mimes))
	for _, mime := range mimes {
		if (mime != "image/jpeg" && mime != "image/png") || wanted[mime] {
			return nil, ErrInvalid
		}
		wanted[mime] = true
	}
	result := make([]DeliverySigningPolicyChoice, 0)
	for id, installed := range r.policies {
		if now.Before(installed.from) || !now.Before(installed.until) {
			continue
		}
		policy, err := DecodeSigningPolicy(installed.body, installed.ref, id)
		if err != nil {
			return nil, err
		}
		allowed := make(map[string]bool, len(policy.AllowedMIME))
		for _, mime := range policy.AllowedMIME {
			allowed[mime] = true
		}
		matches := true
		for mime := range wanted {
			if !allowed[mime] {
				matches = false
				break
			}
		}
		if !matches {
			continue
		}
		operatorPolicy, err := DecodeSigningPolicy(installed.body, installed.ref, id)
		if err != nil {
			return nil, err
		}
		if err := r.authority.CheckSigningOperator(ctx, operatorPolicy); err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			continue
		}
		result = append(result, DeliverySigningPolicyChoice{ID: id, SHA256: installed.ref.SHA256, Revision: policy.Revision})
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID.String() < result[j].ID.String() })
	return result, nil
}

func (r *SigningPolicyRegistry) available(ctx context.Context, mime string, now time.Time) ([]SigningPolicyChoice, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r == nil || r.authority == nil || now.IsZero() || (mime != "image/jpeg" && mime != "image/png") {
		return nil, ErrInvalid
	}
	result := make([]SigningPolicyChoice, 0)
	for id, installed := range r.policies {
		if now.Before(installed.from) || !now.Before(installed.until) {
			continue
		}
		policy, err := DecodeSigningPolicy(installed.body, installed.ref, id)
		if err != nil {
			return nil, err
		}
		allowed := false
		for _, candidate := range policy.AllowedMIME {
			if candidate == mime {
				allowed = true
			}
		}
		if !allowed {
			continue
		}
		if err := r.authority.CheckSigningOperator(ctx, policy); err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			continue
		}
		result = append(result, SigningPolicyChoice{ID: id, Revision: policy.Revision})
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID.String() < result[j].ID.String() })
	return result, nil
}

func (s SQLSignExportService) AvailableSigningPolicies(ctx context.Context, who auth.Identity, tid, export uuid.UUID) ([]SigningPolicyChoice, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.API == nil || s.Catalog == nil || s.Preparation.Registry == nil || !s.Preparation.Registry.matchesTenant(tid) || s.Clock == nil {
		return nil, ErrInvalid
	}
	ready, err := s.Catalog.LookupSigningExport(ctx, who, tid, export)
	if err != nil {
		return nil, err
	}
	registry, err := s.Preparation.Registry.ForTenant(tid)
	if err != nil {
		return nil, err
	}
	choices, err := registry.available(ctx, ready.Object.ContentType, s.Clock())
	if err != nil {
		return nil, err
	}
	choices, err = filterSigningChoicesByAdopter(ctx, s.API, s.Bucket, registry, choices)
	if err != nil {
		return nil, err
	}
	current, err := s.Catalog.LookupSigningExport(ctx, who, tid, export)
	if err != nil {
		return nil, err
	}
	first, err := canonical.Marshal(ready)
	if err != nil {
		return nil, err
	}
	last, err := canonical.Marshal(current)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(first, last) {
		return nil, ErrInvalid
	}
	return choices, nil
}

func (s SQLSignExportService) deliveryProjectVisible(ctx context.Context, who auth.Identity, tenant, project uuid.UUID) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.API == nil || tenant == uuid.Nil || project == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return ErrInvalid
	}
	var allowed bool
	err := database.TenantTransaction(ctx, s.API, tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT COALESCE(photo.v2_editor_authorized($1,$2,$3,$4),false)`, tenant, project, who.UserID, who.SessionID).Scan(&allowed)
	})
	if err != nil {
		return err
	}
	if !allowed {
		return domain.NewError("NOT_FOUND", 404)
	}
	return ctx.Err()
}

// AvailableDeliverySigningPolicies is a redacted advisory catalogue. The
// caller supplies the closed MIME set of selected presets; CreateDelivery
// independently freezes their actual configurations and rechecks policy SHA.
func (s SQLSignExportService) AvailableDeliverySigningPolicies(ctx context.Context, who auth.Identity, tenant, project uuid.UUID, mimes []string) ([]DeliverySigningPolicyChoice, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.Preparation.Registry == nil || !s.Preparation.Registry.matchesTenant(tenant) || s.Clock == nil {
		return nil, ErrInvalid
	}
	if err := s.deliveryProjectVisible(ctx, who, tenant, project); err != nil {
		return nil, err
	}
	registry, err := s.Preparation.Registry.ForTenant(tenant)
	if err != nil {
		return nil, err
	}
	first := s.Clock()
	choices, err := registry.availableDelivery(ctx, mimes, first)
	if err != nil {
		return nil, err
	}
	if err := s.deliveryProjectVisible(ctx, who, tenant, project); err != nil {
		return nil, err
	}
	last := s.Clock()
	if last.Before(first) || last.IsZero() {
		return nil, ErrInvalid
	}
	return currentDeliverySigningChoices(ctx, registry, choices, last)
}

// The project authorization round trip can outlive an operator key or its
// qualification. Recheck both immediately before exposing advisory choices.
func currentDeliverySigningChoices(ctx context.Context, registry *SigningPolicyRegistry, choices []DeliverySigningPolicyChoice, now time.Time) ([]DeliverySigningPolicyChoice, error) {
	current := make([]DeliverySigningPolicyChoice, 0, len(choices))
	for _, choice := range choices {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		installed := registry.policies[choice.ID]
		if now.Before(installed.from) || !now.Before(installed.until) || installed.ref.SHA256 != choice.SHA256 {
			continue
		}
		policy, err := DecodeSigningPolicy(installed.body, installed.ref, choice.ID)
		if err != nil {
			return nil, err
		}
		if err := registry.authority.CheckSigningOperator(ctx, policy); err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			continue
		}
		current = append(current, choice)
	}
	return current, ctx.Err()
}
