package maintenance

import (
	"context"
	"errors"
	"time"

	"photoeditor/backend/internal/artifactjson"
)

// Collection cleanup is a mandatory maintenance loop. Refuse startup when its
// forward-only schema or private maintenance entry points are unavailable.
func RequireCollectionCleanup(ctx context.Context, db artifactjson.RowQuerier) error {
	if err := ctx.Err(); err != nil { return err }
	if db == nil { return errors.New("collection cleanup database required") }
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var ready bool
	err := db.QueryRow(bounded, `SELECT current_user='photo_maintenance'
 AND EXISTS(SELECT FROM pg_attribute a WHERE a.attrelid='photo.deletion_jobs'::regclass
  AND a.attname='collection_cleanup_retry_at' AND NOT a.attisdropped AND a.atttypid='timestamptz'::regtype)
 AND (SELECT count(*)=2 FROM (VALUES
  ('photo.claim_collection_deletion()','record'::regtype::oid),
  ('photo.finish_collection_deletion(uuid,uuid,uuid,uuid)','boolean'::regtype::oid)
 ) expected(signature,result_type)
 JOIN pg_proc p ON p.oid=to_regprocedure(expected.signature) AND p.prorettype=expected.result_type
 WHERE p.prosecdef AND pg_get_userbyid(p.proowner)='photo_fn_owner'
 AND p.prolang=(SELECT oid FROM pg_language WHERE lanname='plpgsql')
 AND replace(array_to_string(p.proconfig,','),' ','')='search_path=pg_catalog,photo'
 AND has_function_privilege(current_user,p.oid,'EXECUTE')
 AND NOT EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a
  WHERE a.grantee NOT IN(p.proowner,(SELECT oid FROM pg_roles WHERE rolname='photo_maintenance'))
  AND a.privilege_type='EXECUTE'))`).Scan(&ready)
	if err != nil { return err }
	if err := bounded.Err(); err != nil { return err }
	if !ready { return errors.New("collection cleanup schema or maintenance privileges unavailable") }
	return nil
}
