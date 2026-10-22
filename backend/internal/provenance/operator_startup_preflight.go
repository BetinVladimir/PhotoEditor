package provenance

import (
	"context"
	"time"
)

// preflightActiveSigningPolicies proves that every currently selectable policy
// can reach its live operator at installation time. The operator performs the
// possession challenge and independent native, X.509, CRL and TSA checks.
// Future policies are checked when they become selectable by Resolve.
func preflightActiveSigningPolicies(ctx context.Context, installation SigningInstallation,
	now time.Time, timeout time.Duration, operator SigningOperatorAuthority) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if now.IsZero() || timeout < time.Second || timeout > 30*time.Second || operator == nil {
		return ErrInvalid
	}
	for _, installed := range installation.Policies {
		if err := ctx.Err(); err != nil {
			return err
		}
		policy, err := decodeInstalledSigningPolicy(installed)
		if err != nil || installed.NotBefore.IsZero() || !installed.NotAfter.After(installed.NotBefore) {
			return ErrInvalid
		}
		if now.Before(installed.NotBefore) || !now.Before(installed.NotAfter) {
			continue
		}
		checkCtx, cancel := context.WithTimeout(ctx, timeout)
		err = operator.CheckSigningOperator(checkCtx, policy)
		cancel()
		if err != nil {
			return err
		}
	}
	return ctx.Err()
}
