package interchange
import (
 "context"
 "os"
 "strings"
 "testing"
 "github.com/jackc/pgx/v5/pgxpool"
)
func TestIntegrationBodyExportCatalogueInstallation(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated DB migrations01566/01568/01570 required")}
 ctx:=context.Background();db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 for _,signature:=range []string{
  "photo.v30_interchange_export_document(uuid,uuid,uuid,text,text,uuid,text,text)",
  "photo.v30_interchange_export_result_input(uuid,uuid,uuid,text,text,uuid,text,uuid,text,text)",
 }{
  var definition,owner string;var definer,public bool
  err:=db.QueryRow(ctx,`SELECT pg_get_functiondef(p.oid),pg_get_userbyid(p.proowner),p.prosecdef,
   EXISTS(SELECT 1 FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a WHERE a.grantee=0 AND a.privilege_type='EXECUTE')
   FROM pg_proc p WHERE p.oid=$1::regprocedure`,signature).Scan(&definition,&owner,&definer,&public)
  if err!=nil{t.Fatal(err)}
  if owner!="photo_fn_owner"||!definer||public{t.Fatal("catalogue security changed",signature)}
  for _,contract:=range []string{"'PoseSet'","'BodyFit'","'PairAlignment'","'FaceBoxes'","'FaceLandmarks'","'Stats20'","'SeriesCorrections'","'AbsoluteLUTArray'","'ResolvedStyle'","'DefectLabelMap'"}{if !strings.Contains(definition,contract){t.Fatal("missing contract",signature,contract)}}
  if !strings.Contains(definition,"a.object_key"){t.Fatal("object binding missing")}
  if strings.Contains(signature,"result_input")&&!strings.Contains(definition,"'DefectLabelMap','AbsoluteLUTArray') THEN a.media_type"){t.Fatal("LUT media branch missing")}
 }
}
