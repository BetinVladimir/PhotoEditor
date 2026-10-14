package interchange

import (
	"context"
	"crypto/ed25519"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

var schedulerExportSQLFunctions = []string{
	"photo.v30_claim_next_export_adoption()",
	"photo.v30_renew_export_adoption_claim(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid)",
	"photo.v30_export_adoption_claim_current(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid)",
	"photo.v30_export_adoption_root_output(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid)",
	"photo.v30_export_adoption_output(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,text)",
	"photo.v30_export_adoption_frozen(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid)",
	"photo.v30_export_adoption_result_input(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,text)",
	"photo.v30_export_adoption_person_origin_inputs(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid)",
	"photo.v30_export_adoption_person_origin_current(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,uuid,text,uuid,text,uuid,text,uuid,integer,bigint,uuid,text)",
	"photo.v30_export_adoption_working_array(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,text,text)",
	"photo.v30_export_adoption_working_warp(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,text)",
	"photo.v30_export_adoption_mask_array(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,text)",
	"photo.v30_export_adoption_mask_warp(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,text,uuid,text)",
	"photo.v30_export_adoption_defect_label(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,text,uuid,text)",
	"photo.v30_export_adoption_series(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,text,text,text,bigint)",
	"photo.v30_export_adoption_series_origin(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,text,text,text,bigint)",
	"photo.v30_export_adoption_series_stat(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,integer,bigint,uuid,text,text,text,text,bigint,uuid,text)",
	"photo.v30_export_adoption_series_stat_image(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,integer,bigint,uuid,text,text,text,text,bigint,uuid,text,uuid,text)",
	"photo.v30_export_adoption_series_measure_origin(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,integer,bigint,uuid,text,text,text,text,bigint,uuid,text,text,text,bigint)",
	"photo.v30_export_adoption_series_measure_inputs(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,integer,bigint,uuid,text,uuid,text,text,text,text,bigint,jsonb)",
	"photo.v30_export_adoption_series_mask(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,integer,bigint,uuid,text,uuid,text,text,text,text,bigint,jsonb,uuid,text,uuid,text)",
	"photo.v30_export_adoption_series_mask_origin(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,integer,bigint,uuid,text,uuid,text,text,text,text,bigint,jsonb,uuid,text,uuid,text)",
	"photo.v30_export_adoption_stats_origin(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,text,text,text,text,bigint)",
	"photo.v30_export_adoption_resolved_origin(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,text,jsonb,text,text,text,bigint)",
	"photo.v30_export_adoption_operation_params(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,text,uuid,text,text,text,text,bigint,jsonb)",
	"photo.v30_export_adoption_correction_member(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,text,uuid,text,text,text,bigint)",
	"photo.v30_export_adoption_stats_person_origin(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,text,uuid,text,uuid,text,text)",
	"photo.v30_export_adoption_stats_person_document(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,text,uuid,text,text,text,text,bigint,text,text,uuid,text,text)",
	"photo.v30_export_adoption_correction_evidence(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,text,text,text,text,bigint)",
	"photo.v30_export_adoption_learning_occurrence(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,uuid,text,jsonb,text,text,uuid,text,text,text,text,bigint,text)",
	"photo.v30_export_adoption_render_frame(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,text,uuid,jsonb,text,integer,uuid,uuid,text,text,uuid,text,text,text,text,bigint)",
	"photo.v30_export_adoption_render_frame_array(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,text,uuid,jsonb,text,integer,uuid,uuid,text,text,uuid,text,text,text,text,bigint)",
	"photo.v30_export_adoption_prompt_qualification_object(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,jsonb,uuid,text)",
	"photo.v30_export_adoption_prompt_profile(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,integer,bigint,uuid,text,uuid,text,uuid,text,text,text,bigint,text,text,bigint,text,uuid,text,uuid,jsonb,text)",
	"photo.v30_export_adoption_person_qualification_object(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,jsonb,uuid,text)",
	"photo.v30_export_adoption_person_profile(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,text,uuid,text,text,text,bigint,text,uuid,text,uuid,jsonb,text)",
	"photo.v30_export_adoption_body_dependency_qualification_object(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,jsonb,uuid,text)",
	"photo.v30_export_adoption_body_dependency_origin(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,text,text,text,uuid,text,text,text,text,bigint)",
	"photo.v30_export_adoption_body_dependency_profile(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,integer,bigint,jsonb)",
	"photo.v30_export_adoption_body_fit_inventory(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,text,uuid,integer,bigint,uuid,jsonb,uuid,text,uuid,text,text,text,text,bigint)",
	"photo.v30_export_adoption_body_alignment_origin(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,text,text,text,bigint)",
	"photo.v30_export_adoption_body_alignment_qualification_object(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,jsonb,uuid,text)",
	"photo.v30_export_adoption_body_alignment_profile(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,integer,bigint,jsonb)",
	"photo.v30_export_adoption_alignment_background_object(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,integer,bigint,uuid,uuid,text,text)",
	"photo.v30_export_adoption_alignment_background_origin(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,integer,bigint,uuid,uuid,text,uuid,text,text)",
	"photo.v30_export_adoption_lut_origin(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,text,uuid,text)",
	"photo.v30_export_adoption_params_mask(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,text,uuid,text,uuid,text)",
	"photo.save_v30_export_adoption_evidence(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,text,text)",
	"photo.adopt_v30_export_adoption(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,text,text)",
}

// SchedulerExportAdoptionSQLReady verifies the exact private function surface
// before any lease can be claimed. A missing migration or broadened PUBLIC ACL
// keeps the runtime unavailable.
func SchedulerExportAdoptionSQLReady(ctx context.Context, db *pgxpool.Pool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if db == nil {
		return v2input.ErrInvalid
	}
	var role string
	var count int
	var ready bool
	err := db.QueryRow(ctx, `SELECT current_user,count(*),coalesce(bool_and(
  p.prosecdef AND owner.rolname='photo_fn_owner'
  AND has_function_privilege(current_user,p.oid,'EXECUTE')
  AND NOT EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) acl
   WHERE acl.grantee=0 AND acl.privilege_type='EXECUTE')
 ),false)
 FROM unnest($1::text[]) signatures(signature)
 JOIN pg_proc p ON p.oid=to_regprocedure(signatures.signature)
 JOIN pg_roles owner ON owner.oid=p.proowner`, schedulerExportSQLFunctions).
		Scan(&role, &count, &ready)
	if err != nil {
		return err
	}
	if role != "photo_scheduler" || count != len(schedulerExportSQLFunctions) || !ready {
		return errors.New("scheduler export adoption SQL authority unavailable")
	}
	return ctx.Err()
}

// BuildSchedulerExportAdoptionProcessor installs the exact object, origin and
// native authorities. It refuses a partial bundle installation at startup.
func BuildSchedulerExportAdoptionProcessor(ctx context.Context, db *pgxpool.Pool,
	objects *objectstore.Store, bucket string, scratch *ExportArchiveScratch,
	trust map[PortablePersonOriginKey]ed25519.PublicKey,
	native SchedulerExportNativeResolverFactory) (SchedulerExportAdoptionProcessor, error) {
	var zero SchedulerExportAdoptionProcessor
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if objects == nil || scratch == nil || native == nil || len(trust) == 0 ||
		len(bucket) < 3 || len(bucket) > 63 {
		return zero, v2input.ErrInvalid
	}
	trusted, err := NewExportPersonOriginArchiveAuthority(
		map[string]PortablePersonOriginExpectation{}, trust, nil)
	if err != nil {
		return zero, err
	}
	if err := SchedulerExportAdoptionSQLReady(ctx, db); err != nil {
		return zero, err
	}
	claims := SQLExportAdoptionClaimStore{DB: db}
	result := SchedulerExportResultReader{Claims: claims, Objects: objects, Bucket: bucket}
	archive := SchedulerExportArchiveReader{ResultReader: result, Scratch: scratch,
		Origins: SQLSchedulerExportPersonOriginInstallation{Claims: claims, Bucket: bucket, Trusted: trusted.trusted}}
	handler := SQLSchedulerExportAdoptionHandler{Claims: claims,
		Reader: SchedulerExportClosureReader{Archive: archive, Native: native}}
	return SchedulerExportAdoptionProcessor{Claims: claims, Handler: handler,
		HeartbeatInterval: 5 * time.Minute}, ctx.Err()
}
