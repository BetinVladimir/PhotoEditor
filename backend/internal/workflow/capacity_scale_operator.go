package workflow

import (
	"context"
	"errors"
	"regexp"

	"github.com/google/uuid"
)

var capacityPolicySHA = regexp.MustCompile(`^[0-9a-f]{64}$`)

// NodeProvisioner is installed by deployment configuration. Its implementation
// must enforce the supplied maximum against authoritative provider state and
// persist idempotency across process restarts before creating a paid node.
type NodeProvisioner interface {
	ProvisionNode(context.Context, string, string, string, int) (string, error)
}

type ScaleUpRequest struct {
	Pool           string
	PolicySHA      string
	IdempotencyKey string
	CurrentNodes   int
	MaximumNodes   int
	LocalOnly      bool
	Decision       ScaleDecision
}

// ApplyScaleUp passes only an already admitted single-node decision to a
// configured operator provisioner. Job parameters and tenant input cannot
// install a provisioner or override the maximum node count.
func ApplyScaleUp(ctx context.Context, request ScaleUpRequest, provisioner NodeProvisioner) (string, error) {
	if request.Pool != "cpu" && request.Pool != "gpu" && request.Pool != "signer" ||
		!capacityPolicySHA.MatchString(request.PolicySHA) ||
		request.MaximumNodes < 1 || request.CurrentNodes < 1 || request.CurrentNodes >= request.MaximumNodes ||
		request.LocalOnly || request.Decision.Alert || request.Decision.Delta != 1 ||
		(request.Decision.Reason != "eligible_wait" && request.Decision.Reason != "reserved_utilization") {
		return "", errors.New("scale-up not admitted")
	}
	key, err := uuid.Parse(request.IdempotencyKey)
	if err != nil || key.String() != request.IdempotencyKey || key == uuid.Nil {
		return "", errors.New("invalid scale-up idempotency key")
	}
	if provisioner == nil {
		return "", errors.New("operator provisioner unavailable")
	}
	node, err := provisioner.ProvisionNode(ctx, request.Pool, request.PolicySHA, request.IdempotencyKey, request.MaximumNodes)
	if err != nil {
		return "", err
	}
	if !validDrainNode(node) {
		return "", errors.New("operator provisioner returned invalid node")
	}
	return node, nil
}
