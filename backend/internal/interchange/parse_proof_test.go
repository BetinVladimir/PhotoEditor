package interchange

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "strings"
 "testing"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/v2input"
)
type proofRow bool
func(r proofRow)Scan(dest ...any)error{*dest[0].(*bool)=bool(r);return nil}
type proofTx struct{args []any;accepted bool;runtimeUnavailable bool}
func(tx *proofTx)QueryRow(_ context.Context,query string,args ...any)pgx.Row{
 if strings.Contains(query,"v30_import_runtime_ready"){return proofRow(!tx.runtimeUnavailable)}
 tx.args=args;return proofRow(tx.accepted)
}
func TestParseProofExactParamsAndIdentity(t *testing.T){
 params:=ParseParams{1,ImportRequest{uuid.New(),"import","photoeditor_bundle",v2input.Ref{ArtifactID:uuid.New(),SHA256:"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}},1024,10}
 raw,err:=CompileParseParams(params);if err!=nil{t.Fatal(err)};sum:=sha256.Sum256(raw)
 scope:=ParseProofScope{uuid.New(),uuid.New(),uuid.New(),v2input.Ref{ArtifactID:uuid.New(),SHA256:hex.EncodeToString(sum[:])}}
 who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()};tx:=&proofTx{accepted:true}
 if err:=SaveParseProof(context.Background(),tx,who,scope,params);err!=nil{t.Fatal(err)}
 if len(tx.args)!=9||tx.args[0]!=scope.Tenant||tx.args[1]!=who.UserID||tx.args[2]!=who.SessionID||tx.args[3]!=params.Request.ProjectID||tx.args[8]!=string(raw){t.Fatal("wrong proof scope")}
 tx.args=nil;changed:=params;changed.MaxFiles++
 if SaveParseProof(context.Background(),tx,who,scope,changed)==nil||tx.args!=nil{t.Fatal("substituted Params queried SQL")}
 tx.accepted=false
 if SaveParseProof(context.Background(),tx,who,scope,params)==nil{t.Fatal("SQL refusal ignored")}
 tx.args=nil;ctx,cancel:=context.WithCancel(context.Background());cancel()
 if SaveParseProof(ctx,tx,who,scope,params)!=context.Canceled||tx.args!=nil{t.Fatal("cancelled SQL")}
}
