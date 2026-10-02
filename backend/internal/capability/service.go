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

type Service struct{ DB *pgxpool.Pool }
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
			var backends []string
			if err = json.Unmarshal(item.AvailableBackends, &backends); err != nil {
				return err
			}
			var supported []string
			if err = json.Unmarshal(item.SupportedBackends, &supported); err != nil {
				return err
			}
			item.Status, item.Reason = classifyBackends(supported, backends)
			item.Available = item.Status == "ready"
			out.Operations = append(out.Operations, item)
		}
		return rows.Err()
	})
	return out, err
}

func classifyBackends(supported, available []string) (string, *string) {
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
