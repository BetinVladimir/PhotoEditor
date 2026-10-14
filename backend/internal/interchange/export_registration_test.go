package interchange

import (
 "context"
 "encoding/json"
 "sort"
 "strings"
 "testing"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
type exportRegistrationTx struct{pgx.Tx;calls int;deny bool;roots []v2input.Ref}
func(tx *exportRegistrationTx)QueryRow(_ context.Context,query string,args ...any)pgx.Row{
 tx.calls++
 if strings.Contains(query,"register_v2_task_input_batch"){
  return ledgerRow(func(dest ...any)error{var rows []v2input.Entry;if err:=json.Unmarshal(args[4].(json.RawMessage),&rows);err!=nil{return err};*dest[0].(*int)=len(rows);return nil})
 }
 if strings.Contains(query,"seal_v2_task_inputs"){
  return ledgerRow(func(dest ...any)error{if err:=json.Unmarshal(args[7].(json.RawMessage),&tx.roots);err!=nil{return err};*dest[0].(*bool)=!tx.deny;return nil})
 }
 panic("unexpected registration query")
}
func TestExportRegistrationBindsFrozenRootsAndPublishedDigest(t *testing.T){
 tid,_,intent,compiled,ref,raw:=exportLedgerFixture(t)
 frozen,err:=FreezeExportDocuments(context.Background(),intent,compiled,ref,raw);if err!=nil{t.Fatal(err)}
 entries:=compiled.InputRoots();sort.Slice(entries,func(i,j int)bool{return entries[i].Ref.ArtifactID.String()<entries[j].Ref.ArtifactID.String()})
 digest,err:=canonical.Hash(entries);if err!=nil{t.Fatal(err)}
 inventory:=ExportInputInventory{task:compiled.TaskID(),entries:entries,digest:digest}
 transport,err:=inventory.PublishInputs(context.Background(),&transportPublisher{});if err!=nil{t.Fatal(err)}
 tx:=&exportRegistrationTx{}
 if err:=transport.RegisterInputs(context.Background(),tx,tid,intent,frozen);err!=nil{t.Fatal(err)}
 if tx.calls!=2||len(tx.roots)!=3||tx.roots[0]!=compiled.paramsRef||tx.roots[1]!=compiled.planRef||tx.roots[2]!=ref{t.Fatal("wrong registration roots")}
 bad:=transport;bad.built.SHA256=rawSHA([]byte("wrong"));rejected:=&exportRegistrationTx{}
 if err:=bad.RegisterInputs(context.Background(),rejected,tid,intent,frozen);err==nil||rejected.calls!=0{t.Fatal("invalid receipt reached SQL")}
 if err:=transport.RegisterInputs(context.Background(),&exportRegistrationTx{deny:true},tid,intent,frozen);err==nil{t.Fatal("failed input seal accepted")}
}
