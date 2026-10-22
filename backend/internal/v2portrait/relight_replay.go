package v2portrait

import "context"

type relightReplayOnlyKey struct{}

// WithRelightReplayOnly permits historical receipt reads but prohibits fresh
// draft creation, parameter publication and admission after confirmed absence.
// The flag never substitutes for the reader's current authorization checks.
func WithRelightReplayOnly(ctx context.Context) context.Context {
 return context.WithValue(ctx, relightReplayOnlyKey{}, true)
}
// IsRelightReplayOnly reports whether this call must stop after receipt lookup.
func IsRelightReplayOnly(ctx context.Context) bool {
 value, _ := ctx.Value(relightReplayOnlyKey{}).(bool)
 return value
}
