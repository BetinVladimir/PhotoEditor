package main

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
)

// API outputs and status require these boundaries even without GPU admission.
func requireRunPresentationSchema(ctx context.Context, api *pgxpool.Pool) error {
	if api == nil {
		return errors.New("run presentation database unavailable")
	}
	var ready bool
	err := api.QueryRow(ctx, `SELECT current_user='photo_api' AND bool_and(
 to_regprocedure(signature) IS NOT NULL AND COALESCE(has_function_privilege(current_user,to_regprocedure(signature),'EXECUTE'),false))
 FROM unnest(ARRAY['photo.v26_render_derivative_outputs(uuid,uuid,uuid,uuid)',
 'photo.v26_render_presentation_status(uuid,uuid,uuid,uuid)']) signature`).Scan(&ready)
	if err != nil {
		return err
	}
	if !ready {
		return errors.New("run presentation migrations or API privileges unavailable")
	}
	return nil
}
