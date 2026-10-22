package httpapi

import (
 "context"
 "errors"
 "time"
)

type readinessDatabase interface { Ping(context.Context) error }

// The API remains independent of optional GPU availability. Each task admission
// checks its actual qualification; health only checks essential API dependencies.
func checkReadiness(parent context.Context, database readinessDatabase, runtime func(context.Context) error) error {
 if database == nil || runtime == nil { return errors.New("readiness installation incomplete") }
 ctx, cancel := context.WithTimeout(parent, 3*time.Second)
 defer cancel()
 if err := ctx.Err(); err != nil { return err }
 if err := database.Ping(ctx); err != nil { return err }
 if err := ctx.Err(); err != nil { return err }
 if err := runtime(ctx); err != nil { return err }
 return ctx.Err()
}
