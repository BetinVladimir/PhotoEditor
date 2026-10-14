package delivery

import (
	"context"
	"sort"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/exportpreset"
	"photoeditor/backend/internal/v2input"
)

// CreateDeliveryDependencies keeps API authorization and scheduler transport
// privileges on their existing separate pools. Production configuration must
// supply the existing V26 freezer and scheduler JSON document publisher.
type CreateDeliveryDependencies struct {
	Signing          DeliverySigningPolicyChecker
	MetadataPreparer *PythonMetadataSourcePreparer
	Scheduler        *pgxpool.Pool
	Bucket           string
	Objects          CreatePreparationObjects
	Documents        v2input.DocumentPublisher
	Freezer          CreateRecipeFreezer
	Limits           PrepareResourceLimits
}

type DeliverySigningPolicyChecker interface {
	CheckDeliveryPolicy(context.Context, uuid.UUID, uuid.UUID, string) error
	CheckDeliveryPolicyMIMEs(context.Context, uuid.UUID, uuid.UUID, string, []string) error
}

func selectedDeliveryMIMEs(catalog CreateCatalog) ([]string, error) {
	if len(catalog.Presets) < 1 || len(catalog.Presets) > 16 {
		return nil, domain.NewError("DELIVERY_PROVENANCE_UNAVAILABLE", 503)
	}
	seen := map[uuid.UUID]bool{}
	mimes := map[string]bool{}
	for _, preset := range catalog.Presets {
		if preset.ID == uuid.Nil || seen[preset.ID] {
			return nil, domain.NewError("DELIVERY_PROVENANCE_UNAVAILABLE", 503)
		}
		seen[preset.ID] = true
		config, err := exportpreset.DecodeFrozenConfig(preset.ConfigJCS, preset.SHA256)
		if err != nil {
			return nil, domain.NewError("DELIVERY_PROVENANCE_UNAVAILABLE", 503)
		}
		switch config.Format {
		case "jpeg":
			mimes["image/jpeg"] = true
		case "png":
			mimes["image/png"] = true
		default:
			return nil, domain.NewError("DELIVERY_PROVENANCE_UNAVAILABLE", 503)
		}
	}
	out := make([]string, 0, len(mimes))
	for mime := range mimes {
		out = append(out, mime)
	}
	sort.Strings(out)
	return out, nil
}

func checkCreateSigningMIMEs(ctx context.Context, tid uuid.UUID, policy Policy, catalog CreateCatalog, checker DeliverySigningPolicyChecker) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if policy.Provenance != "required" {
		return nil
	}
	if checker == nil {
		return domain.NewError("DELIVERY_PROVENANCE_UNAVAILABLE", 503)
	}
	selected := make(map[uuid.UUID]bool, len(policy.RequiredPresets)+len(policy.OptionalPresets))
	for _, id := range append(append([]uuid.UUID{}, policy.RequiredPresets...), policy.OptionalPresets...) {
		if id == uuid.Nil || selected[id] {
			return domain.NewError("DELIVERY_PROVENANCE_UNAVAILABLE", 503)
		}
		selected[id] = true
	}
	if len(selected) != len(catalog.Presets) {
		return domain.NewError("DELIVERY_PROVENANCE_UNAVAILABLE", 503)
	}
	for _, preset := range catalog.Presets {
		if !selected[preset.ID] {
			return domain.NewError("DELIVERY_PROVENANCE_UNAVAILABLE", 503)
		}
	}
	mimes, err := selectedDeliveryMIMEs(catalog)
	if err != nil {
		return err
	}
	if err = checker.CheckDeliveryPolicyMIMEs(ctx, tid, policy.SigningPolicyID, policy.SigningPolicySHA, mimes); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return domain.NewError("DELIVERY_PROVENANCE_UNAVAILABLE", 503)
	}
	return ctx.Err()
}

func checkCreateSigningPolicy(ctx context.Context, tid uuid.UUID,
	policy Policy, checker DeliverySigningPolicyChecker) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if policy.Provenance != "required" {
		return nil
	}
	if checker == nil {
		return domain.NewError("DELIVERY_PROVENANCE_UNAVAILABLE", 503)
	}
	if err := checker.CheckDeliveryPolicy(ctx, tid,
		policy.SigningPolicyID, policy.SigningPolicySHA); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return domain.NewError("DELIVERY_PROVENANCE_UNAVAILABLE", 503)
	}
	return ctx.Err()
}

type PreparedCreateDelivery struct {
	Identity  CreateIdentity
	Transport StagedCreateTransport
}

func deliveryPoolsIsolated(api, scheduler *pgxpool.Pool) bool {
	return api != nil && scheduler != nil && api != scheduler
}

// PrepareDelivery connects durable selection, recipe freezing, immutable
// creation/transport publication, scheduler input registration and API proof/
// package admission. Every object effect occurs outside DB transactions.
// Failure may leave durable draft evidence which exact retry resumes.
// This internal phase never seals/activates the graph or completes the public
// idempotency response; the result is not a JobTicket or successful HTTP create.
func (s Service) PrepareDelivery(ctx context.Context, who auth.Identity, tid uuid.UUID, request CreateRequest, key string, deps CreateDeliveryDependencies) (PreparedCreateDelivery, error) {
	if err := ctx.Err(); err != nil {
		return PreparedCreateDelivery{}, err
	}
	intent, err := FreezeCreateIntent(tid, who, request, key)
	if err != nil {
		return PreparedCreateDelivery{}, err
	}
	if err := checkCreateSigningPolicy(ctx, tid, intent.Request().Policy, deps.Signing); err != nil {
		return PreparedCreateDelivery{}, err
	}
	if !deliveryPoolsIsolated(s.DB, deps.Scheduler) || deps.Objects == nil || deps.Documents == nil || deps.Freezer == nil || len(deps.Bucket) < 3 || len(deps.Bucket) > 63 || deps.Limits.RAMBytes <= 0 || deps.Limits.ScratchBytes <= 0 || deps.Limits.OutputBytes < 8<<20 {
		return PreparedCreateDelivery{}, domain.NewError("DELIVERY_UNAVAILABLE", 503)
	}
	if request.Policy.MetadataPolicy != "strip" {
		if deps.MetadataPreparer == nil {
			return PreparedCreateDelivery{}, domain.NewError("DELIVERY_METADATA_UNAVAILABLE", 503)
		}
		if _, err := deps.MetadataPreparer.environment(); err != nil {
			return PreparedCreateDelivery{}, domain.NewError("DELIVERY_METADATA_UNAVAILABLE", 503)
		}
	}
	request = intent.Request()
	pending, err := s.PrepareCreatePending(ctx, who, tid, request, key, deps.Freezer)
	if err != nil {
		return PreparedCreateDelivery{}, err
	}
	if pending.Completed {
		return PreparedCreateDelivery{}, domain.NewError("DELIVERY_CREATE_STALE", 409)
	}
	if err := checkCreateSigningMIMEs(ctx, tid, request.Policy, pending.Pending.Catalog, deps.Signing); err != nil {
		return PreparedCreateDelivery{}, err
	}
	transport, err := s.StageCreateTransport(ctx, who, tid, request, key, deps.Bucket, deps.Objects, deps.Documents, deps.Limits, deps.MetadataPreparer)
	if err != nil {
		return PreparedCreateDelivery{}, err
	}
	err = database.TenantTransaction(ctx, deps.Scheduler, tid, func(tx pgx.Tx) error {
		return transport.Frozen.RegisterInputsInTransaction(ctx, tx, tid, intent.identity.Run, transport.Graph)
	})
	if err != nil {
		return PreparedCreateDelivery{}, err
	}
	if err := s.AdmitPreparedCreate(ctx, who, tid, request, key); err != nil {
		return PreparedCreateDelivery{}, err
	}
	return PreparedCreateDelivery{Identity: intent.identity, Transport: transport}, nil
}
