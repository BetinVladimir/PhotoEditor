package workflow

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2draft"
)

func IdentityAdoptionEnabled(getenv func(string) string) (bool, error) {
	if getenv == nil {
		return false, errors.New("identity adoption environment unavailable")
	}
	switch getenv("PHOTO_V23_IDENTITY_ADOPTION_ENABLED") {
	case "", "false":
		return false, nil
	case "true":
		return true, nil
	default:
		return false, errors.New("invalid identity adoption enabled flag")
	}
}

func NewIdentityAdoptionProcessor(ctx context.Context, db *pgxpool.Pool,
	objects *objectstore.Store, bucket string) (*v2draft.IdentityAdoptionDispatcher, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if db == nil || objects == nil || len(bucket) < 3 || len(bucket) > 63 {
		return nil, errors.New("identity adoption dependencies invalid")
	}
	bounded, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	required := []string{
		"photo.v23_identity_adopter_heartbeat()",
		"photo.next_v23_identity_adoption(uuid)",
		"photo.v23_identity_result_input(uuid,uuid,uuid,uuid,text,text,uuid,text,text)",
		"photo.v23_identity_consent_result_input(uuid,uuid,uuid,uuid,text,uuid,text,text)",
		"photo.v23_identity_calibration_result_input(uuid,uuid,uuid,uuid,text,uuid,text,text)",
		"photo.v23_identity_result_object(uuid,uuid,uuid,uuid,text,text,uuid,text,text)",
		"photo.v23_identity_live_sources(uuid,uuid,uuid,uuid,uuid,text,text,text,uuid,uuid,text,text,text)",
		"photo.v23_identity_live_consents(uuid,uuid,uuid,uuid,text,text,uuid,uuid)",
		"photo.v23_identity_live_qualification(uuid,uuid,uuid,uuid,text,text,uuid,text)",
		"photo.adopt_v23_identity_verified(uuid,uuid,integer,bigint,uuid,jsonb,text,uuid,text,uuid,text,uuid,text)",
	}
	var installed bool
	err := db.QueryRow(bounded, `SELECT current_user='photo_scheduler'
 AND position('v23_identity_adopter_state' in
  pg_get_functiondef('photo.activate_v23_identity_compare(uuid,uuid,uuid,uuid,text)'::regprocedure))>0
 AND coalesce(bool_and(coalesce(p.prosecdef AND owner.rolname='photo_fn_owner'
 AND has_function_privilege(current_user,p.oid,'EXECUTE'),false)),false)
 FROM unnest($1::text[]) required(signature)
 LEFT JOIN pg_proc p ON p.oid=to_regprocedure(signature)
 LEFT JOIN pg_roles owner ON owner.oid=p.proowner`, required).Scan(&installed)
	if err != nil || !installed {
		return nil, errors.New("identity adoption SQL installation unavailable")
	}
	return &v2draft.IdentityAdoptionDispatcher{
		Candidates: v2draft.SQLIdentityAdoptionQueue{DB: db},
		Adopter:    v2draft.SQLIdentityCandidateAdopter{DB: db, Objects: objects, Bucket: bucket},
		Timeout:    10 * time.Minute}, nil
}
