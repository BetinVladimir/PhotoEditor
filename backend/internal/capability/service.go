package capability

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
)

type Service struct {
	DB                *pgxpool.Pool
	SemanticInstalled func(uuid.UUID, string) bool
}
type Operation struct {
	Operation         string          `json:"operation"`
	ResourceClass     string          `json:"resource_class"`
	ModelID           *string         `json:"model_id"`
	SupportedBackends json.RawMessage `json:"supported_backends"`
	AvailableBackends json.RawMessage `json:"available_backends"`
	Available         bool            `json:"available"`
	Status            string          `json:"status"`
	Reason            *string         `json:"reason"`
}
type Capabilities struct {
	Operations []Operation `json:"operations"`
}

func (s Service) Get(ctx context.Context, who auth.Identity, tid uuid.UUID) (Capabilities, error) {
	out := Capabilities{Operations: []Operation{}}
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Viewer, func(tx pgx.Tx, role domain.Role) error {
		rows, err := tx.Query(ctx, `SELECT operation,resource_class,model_id,supported_backends,available_backends FROM photo.tenant_capabilities()`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item Operation
			if err = rows.Scan(&item.Operation, &item.ResourceClass, &item.ModelID, &item.SupportedBackends, &item.AvailableBackends); err != nil {
				return err
			}
			item = normalizeRawBackendEvidence(item)
			item = maskSemanticInstallation(item, tid, s.SemanticInstalled)
			out.Operations = append(out.Operations, item)
		}
		return rows.Err()
	})
	return out, err
}

func classifyBackends(supported, available []string) (string, *string) {
	allowed := make(map[string]bool, len(supported))
	for _, backend := range supported {
		if !knownBackend(backend) || allowed[backend] {
			reason := "invalid_backend_evidence"
			return "unqualified", &reason
		}
		allowed[backend] = true
	}
	seen := make(map[string]bool, len(available))
	for _, backend := range available {
		if !knownBackend(backend) || !allowed[backend] || seen[backend] {
			reason := "invalid_backend_evidence"
			return "unqualified", &reason
		}
		seen[backend] = true
	}
	if len(available) > 0 {
		return "ready", nil
	}
	if len(supported) == 0 {
		reason := "no_supported_backend"
		return "unsupported", &reason
	}
	reason := "no_qualified_worker"
	return "unqualified", &reason
}

func maskSemanticInstallation(item Operation, tenant uuid.UUID, installed func(uuid.UUID, string) bool) Operation {
	switch item.Operation {
	case "semantic.preview_local_exposure", "semantic.adopt_shown_exposure", "semantic.preview_remove_object", "semantic.adopt_shown_removal", "semantic.preview_background_cleanup", "semantic.preview_retouch_region", "semantic.adopt_shown_background_cleanup", "semantic.adopt_shown_retouch_region":
		if installed == nil || !installed(tenant, item.Operation) {
			reason := "api_installation_missing"
			item.Available = false
			item.Status = "unqualified"
			item.Reason = &reason
			item.AvailableBackends = json.RawMessage(`[]`)
		}
	}
	return item
}

// A malformed operation cannot poison the entire tenant snapshot or enable an
// adapter. Preserve valid declared alternatives but remove execution evidence.
func knownBackend(value string) bool {
	return value == "cpu" || value == "cuda" || value == "mps"
}

func normalizeBackendEvidence(item Operation, supported, available []string) Operation {
	item.Status, item.Reason = classifyBackends(supported, available)
	item.Available = item.Status == "ready"
	if item.Available { return item }
	item.AvailableBackends = json.RawMessage(`[]`)
	clean := make([]string, 0, len(supported))
	seen := make(map[string]bool)
	for _, backend := range supported {
		if knownBackend(backend) && !seen[backend] {
			clean = append(clean, backend)
			seen[backend] = true
		}
	}
	item.SupportedBackends, _ = json.Marshal(clean)
	return item
}

// SQL JSON shape errors are local unavailability, never a tenant-wide outage.
func parseBackendEvidence(raw json.RawMessage) ([]string, bool) {
	var values []json.RawMessage
	if len(raw) == 0 || len(raw) > 32768 || json.Unmarshal(raw, &values) != nil || values == nil {
		return nil, false
	}
	valid := len(values) <= 32
	if len(values) > 32 { values = values[:32] }
	out := make([]string, 0, len(values))
	for _, value := range values {
		var backend string
		if len(value) == 0 || value[0] != '"' || json.Unmarshal(value, &backend) != nil {
			valid = false
			continue
		}
		out = append(out, backend)
	}
	return out, valid
}

func normalizeRawBackendEvidence(item Operation) Operation {
	supported, supportedOK := parseBackendEvidence(item.SupportedBackends)
	available, availableOK := parseBackendEvidence(item.AvailableBackends)
	if supportedOK && availableOK { return normalizeBackendEvidence(item, supported, available) }
	// Force invalid classification while retaining only parseable declared
	// alternatives. Invalid available evidence never contributes to readiness.
	item = normalizeBackendEvidence(item, supported, []string{"invalid"})
	return item
}
