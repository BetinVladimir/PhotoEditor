package v2sessions

import (
 "context"
 "os"
 "strings"
 "testing"
 "time"

 "github.com/jackc/pgx/v5/pgxpool"
)

// This installed-catalog scenario is separate from the real Apply fixture.
// It verifies the final ordering and private owner boundary, not lock-race PASS.
func TestIntegrationAnalysisApplyFinalAuthorityInstallation(t *testing.T) {
 dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
 if dsn == "" { t.Skip("isolated migrated PostgreSQL required") }
 ctx,cancel := context.WithTimeout(context.Background(),5*time.Second)
 defer cancel()
 db,err := pgxpool.New(ctx,dsn)
 if err != nil { t.Fatal(err) }
 defer db.Close()
 var source,owner string
 var definer,api,public bool
 err=db.QueryRow(ctx,`SELECT p.prosrc,pg_get_userbyid(p.proowner),p.prosecdef,
 has_function_privilege('photo_api',p.oid,'EXECUTE'),
 EXISTS(SELECT 1 FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a
 WHERE a.grantee=0 AND a.privilege_type='EXECUTE')
 FROM pg_proc p WHERE p.oid=to_regprocedure(
 'photo.finish_v22_analysis_apply(uuid,uuid,uuid,uuid,bigint,text,uuid,text,text,uuid[],uuid)')`).Scan(&source,&owner,&definer,&api,&public)
 if err != nil { t.Fatal(err) }
 cursor:=strings.Index(source,"-- 02567:")
 fence:=strings.Index(source[cursorOrZero(cursor):],"IF NOT FOUND OR NOT photo.v2_editor_authorized(")
 write:=strings.Index(source," INSERT INTO photo.v22_analysis_decisions(")
 parent:=strings.Index(source,"AND r.id=p_child FOR UPDATE;")
 if cursor<0 || fence<0 || parent<0 || parent>=cursor || cursor+fence>=write || !definer || owner!="photo_fn_owner" || !api || public {
  t.Fatalf("final authority ordering/ACL invalid: cursor=%d fence=%d write=%d parent=%d owner=%s",cursor,fence,write,parent,owner)
 }
}

func cursorOrZero(value int) int { if value<0 { return 0 }; return value }
