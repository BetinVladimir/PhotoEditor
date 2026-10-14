package interchange

import (
 "context"
 "os"
 "strings"
 "testing"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2qualification"
)
func TestBodyDependencyQualificationReaderRequiresDatabase(t *testing.T){
 source:=SQLExportDocumentCatalog{}
 if _,err:=source.ReadExportBodyDependencyQualification(context.Background(),nil,v2input.Ref{},v2qualification.Expected{});err!=v2input.ErrInvalid{t.Fatal(err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=source.ReadExportBodyDependencyQualification(ctx,nil,v2input.Ref{},v2qualification.Expected{});err!=context.Canceled{t.Fatal(err)}
}
func TestIntegrationBodyDependencyQualificationInstallation(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated DB migration01567 required")}
 ctx:=context.Background();db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 var definition,owner string;var definer,public bool
 err=db.QueryRow(ctx,`SELECT pg_get_functiondef(p.oid),pg_get_userbyid(p.proowner),p.prosecdef,
 EXISTS(SELECT 1 FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a WHERE a.grantee=0 AND a.privilege_type='EXECUTE')
 FROM pg_proc p WHERE p.oid='photo.v30_body_dependency_qualification_object(uuid,uuid,uuid,uuid,jsonb)'::regprocedure`).Scan(&definition,&owner,&definer,&public)
 if err!=nil{t.Fatal(err)};if owner!="photo_fn_owner"||!definer||public{t.Fatal("qualification function security")}
 for _,predicate:=range []string{"pose.detect","face.landmarks","pair.fit.body","policy.admission_enabled","policy.license_cleared","q.trusted_import","v2_qualification.imported","report.object_version","photo.v2_editor_authorized"}{if !strings.Contains(definition,predicate){t.Fatal("qualification gate missing",predicate)}}
}

func TestBodyDependencyQualificationOperationAllowlist(t *testing.T){
 for _,operation:=range []string{"pose.detect","face.landmarks","pair.fit.body"}{if !exportBodyDependencyQualificationOperation(operation){t.Fatal("supported operation refused",operation)}}
 for _,operation:=range []string{"","mask.prompt","geometry.body","pose.detect/1","face.detect","pair.fit.beauty"}{if exportBodyDependencyQualificationOperation(operation){t.Fatal("foreign operation accepted",operation)}}
}
