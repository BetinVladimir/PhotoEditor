package interchange
import("context";"testing";"github.com/google/uuid";"github.com/jackc/pgx/v5";"photoeditor/backend/internal/v2input")
type collectionRow struct{id *uuid.UUID}
func(r collectionRow)Scan(dest ...any)error{*dest[0].(**uuid.UUID)=r.id;return nil}
type collectionTx struct{id *uuid.UUID;args []any}
func(t *collectionTx)QueryRow(_ context.Context,_ string,args ...any)pgx.Row{t.args=args;return collectionRow{t.id}}
func TestImportCollectionScopePendingAndDeterministicReplay(t *testing.T){
 tenant,job:=uuid.New(),uuid.New();body:=[]byte(`{"schema_version":1}`);ref:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(body)}
 tx:=&collectionTx{}
 if id,err:=PrepareImportCollection(context.Background(),tx,tenant,job,ref,body);err!=nil||id!=uuid.Nil{t.Fatal("pending changed",err)}
 expected:=uuid.NewSHA1(job,[]byte("interchange:draft-collection:1"));tx.id=&expected
 if id,err:=PrepareImportCollection(context.Background(),tx,tenant,job,ref,body);err!=nil||id!=expected||len(tx.args)!=3||tx.args[0]!=tenant||tx.args[1]!=job{t.Fatal("scope changed",err)}
 foreign:=uuid.New();tx.id=&foreign
 if _,err:=PrepareImportCollection(context.Background(),tx,tenant,job,ref,body);err==nil{t.Fatal("foreign collection accepted")}
 tx.args=nil
 if _,err:=PrepareImportCollection(context.Background(),tx,tenant,job,ref,[]byte(`{}`));err==nil||tx.args!=nil{t.Fatal("substituted body queried")}
}
