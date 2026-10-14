package delivery

import (
	"context"

	"photoeditor/backend/internal/v2input"
)

// SignedAssemblyTransportReady acknowledges the exact signed task closure
// after v2_task_inputs registration. Unlike the ordinary transport predicate,
// SQL requires envelope, closure, signed export and C2PA manifest contracts.
func (s ExpansionLeaseStore) SignedAssemblyTransportReady(ctx context.Context,
	lease ExpansionLease, plan PreparePlan, task v2input.DraftTask, paramsRaw []byte,
	) (bool, error) {
	raw, err := s.assemblyTaskDescriptor(ctx, lease, plan, task, paramsRaw)
	if err != nil { return false, err }
	var ready bool
	err = s.DB.QueryRow(ctx, `SELECT photo.v30_delivery_signed_assembly_transport_ready($1,$2,$3::jsonb,$4)`,
		lease.Expansion, lease.Token, string(raw), string(paramsRaw)).Scan(&ready)
	return ready, err
}
