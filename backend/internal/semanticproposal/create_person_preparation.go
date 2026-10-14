package semanticproposal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// Distinct from missing target evidence: only the reference gate may initiate
// this prerequisite. It retains the existing public unavailable classification.
var errCreateReferencePersonUnavailable = fmt.Errorf("reference person prerequisite: %w", errPersonProducerUnavailable)

// Starts only the independently qualified person prerequisite, under the same
// original parent lock used by native recovery. No child receipt leaves this
// boundary. Public retries retain the parent identity and original snapshot.
func (p TransferDraftPreparer) prepareMissingCreatePerson(ctx context.Context, who auth.Identity, request Request, key string, published PublishedIntent) (bool, error) {
	owned, err := ownNativePlanRequest(ctx, who, request, key)
	if err != nil {
		return false, err
	}
	if err := validateCreatePreparer(ctx, p); err != nil {
		return false, err
	}
	capacity := ownTransferPreparation(p.Configuration)
	if capacity.PersonProtection == nil {
		return false, v2input.ErrInvalid
	}
	return withNativeManualParentLock(ctx, p.Scheduler, owned.TenantID, owned.RunID, func(work context.Context) error {
		var value *string
		err := tenant.WithTenant(work, p.Reader.DB, who, owned.TenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
			return tx.QueryRow(work, "SELECT photo.v27_native_person_preparation_presence($1,$2,$3,$4,$5,$6)", owned.TenantID, p.Reader.ProjectID, who.UserID, who.SessionID, owned.RunID, owned.RequestSHA).Scan(&value)
		})
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && value == nil) {
			return v2input.ErrConflict
		}
		if err != nil {
			return err
		}
		presence, err := decodeNativeStagePresence(*value)
		if err != nil {
			return err
		}
		if presence == nativeStageConflict {
			return v2input.ErrConflict
		}
		catalog := v2draft.SQLArtifactCatalog{DB: p.Reader.DB, Bucket: p.Reader.Bucket, ProjectID: p.Reader.ProjectID, AssetVersionID: owned.Intent.Reference.AssetVersionID}
		run, _, err := personPreparationIDs(owned.RunID)
		if err != nil {
			return err
		}
		publisher := v2input.ScopedPublisher{Documents: p.TransportDocuments, Tenant: owned.TenantID, Run: run}
		if presence == nativeStagePresent {
			anchor, err := p.Reader.loadPersonPreparationAnchor(work, who, owned.TenantID, owned.RunID, capacity.Color, capacity.MetadataBytes)
			if err != nil {
				return err
			}
			if anchor.Key != key || !bytes.Equal(anchor.Request.Body, owned.Body) || !sameTransferInventory(anchor.Published.Inventory, published.Inventory) {
				return v2input.ErrConflict
			}
			plan, err := p.Reader.restorePersonDraftPlan(work, catalog, who, anchor)
			if err != nil {
				return err
			}
			expected, err := canonical.Marshal(capacity.PersonProtection.Expected)
			if err != nil {
				return err
			}
			if plan.prepared.qualification.report.ref != capacity.PersonProtection.Report || plan.prepared.qualification.expected != string(expected) {
				return v2qualification.ErrUnqualified
			}
			state, err := p.Reader.readNativeInputSavedStageState(work, who, owned, published.Ref, anchor.Snapshot, "mask.person")
			if err != nil {
				return err
			}
			if !state.sealed {
				if _, err := p.Reader.persistPersonPreparationInputs(work, p.Scheduler, publisher, who, plan, capacity.Color); err != nil {
					return err
				}
			}
			_, err = p.Reader.resumePersonPreparation(work, p.Scheduler, who, plan)
			return err
		}
		// Verify the deployment-pinned report and its measured costs before publishing
		// any new snapshot or Params. No invented resource costs enable an unmeasured model.
		policy := capacity.PersonProtection
		pin, err := p.Reader.personQualificationObject(work, who, owned.TenantID, policy.Expected, policy.Report)
		if err != nil {
			return err
		}
		if pin.object.Bytes < 2 || pin.object.Bytes > capacity.MetadataBytes || pin.object.Bytes > 8<<20 {
			return v2input.ErrInvalid
		}
		raw, err := v2input.ReadPinnedSemanticDocument(work, p.Reader.Objects, pin.object, policy.Report)
		if err != nil {
			return err
		}
		verified, err := v2qualification.Verify(raw, policy.Report.SHA256, policy.Expected)
		if err != nil {
			return err
		}
		costs, err := transferCostsFromVerifiedMetrics(verified.Metrics)
		if err != nil {
			return err
		}
		if costs.VRAMBytes <= 0 || policy.Expected.Backend != "cuda" {
			return v2qualification.ErrUnqualified
		}
		budget, err := createPersonBudget(capacity, p.ReservedBytes, verified.Metrics)
		if err != nil {
			return err
		}
		budget.OrdinaryPolicy = ordinaryParentPolicy(p)
		after, err := p.Reader.personQualificationObject(work, who, owned.TenantID, policy.Expected, policy.Report)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(pin, after) {
			return v2input.ErrConflict
		}
		snapshot, body, err := p.Reader.PublishTargetSnapshot(work, p.Documents, who, owned, key, published)
		if err != nil {
			return err
		}
		anchor := nativePreparationAnchor{Request: owned, Key: key, Published: published, Snapshot: snapshot, SnapshotBody: append(json.RawMessage(nil), body...), Color: capacity.Color}
		bootstrap := nativePreparationBootstrap{Reader: p.Reader, Scheduler: p.Scheduler, Catalog: catalog, Documents: p.Documents, Transport: publisher}
		_, _, err = bootstrap.bootstrapPerson(work, who, anchor, policy.Report, 512, budget)
		return err
	})
}

// Bound the compiler output ceiling by the actual child reservation, retaining
// the transport allowance. The compiled person profile must still fit this
// ceiling; no output or image is truncated to make it fit.
func createPersonBudget(capacity TransferPreparation, reserved int64, metrics v2qualification.Metrics) (nativePreparationBudget, error) {
	if err := validateTransferCapacityConfiguration(capacity); err != nil {
		return nativePreparationBudget{}, err
	}
	if reserved <= 8<<20 {
		return nativePreparationBudget{}, v2input.ErrInvalid
	}
	capacity = ownTransferPreparation(capacity)
	if capacity.Output > reserved-(8<<20) {
		capacity.Output = reserved - (8 << 20)
	}
	budget, err := initialNativeFaceBudget(capacity, reserved, metrics)
	return budget, err
}
