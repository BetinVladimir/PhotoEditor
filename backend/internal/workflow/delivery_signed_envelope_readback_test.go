package workflow

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/v2input"
)

type signedEnvelopeReadRow struct { raw,sha string; err error }
func(r signedEnvelopeReadRow)Scan(dest ...any)error{
	if r.err!=nil{return r.err}
	*dest[0].(*string)=r.raw;*dest[1].(*string)=r.sha;return nil
}
type signedEnvelopeReadDB struct{ row signedEnvelopeReadRow; calls int; args []any }
func(d *signedEnvelopeReadDB)QueryRow(_ context.Context,_ string,args ...any)pgx.Row{
	d.calls++;d.args=args;return d.row
}

func TestReadSignedDeliveryEnvelopeBindsLeaseAndCanonicalRefs(t *testing.T){
	lease:=delivery.ExpansionLease{Expansion:uuid.New(),Token:uuid.New(),Until:time.Now().Add(time.Minute),
		Tenant:uuid.New(),Run:uuid.New(),Workflow:uuid.New(),Package:uuid.New(),ParentTask:uuid.New(),Cursor:1,SlotCount:1}
	assembly,closure,envelope:=uuid.New(),uuid.New(),uuid.New()
	sha:=renderDerivativeBytesSHA([]byte("assembly"))
	closureSHA:=renderDerivativeBytesSHA([]byte("closure"))
	raw:=[]byte(`{"assembly":{"artifact_id":"`+assembly.String()+`","sha256":"`+sha+`"},"schema_version":2,"signing_closure":{"artifact_id":"`+closure.String()+`","sha256":"`+closureSHA+`"}}`)
	ref:=v2input.Ref{ArtifactID:envelope,SHA256:renderDerivativeBytesSHA(raw)}
	db:=&signedEnvelopeReadDB{row:signedEnvelopeReadRow{raw:string(raw),sha:ref.SHA256}}
	got,body,err:=ReadSignedDeliveryEnvelope(context.Background(),db,lease,ref)
	if err!=nil||string(body)!=string(raw)||got.Assembly.ArtifactID!=assembly||got.SigningClosure.ArtifactID!=closure||
		len(db.args)!=4||db.args[0]!=lease.Expansion||db.args[1]!=lease.Token||db.args[2]!=ref.ArtifactID||db.args[3]!=ref.SHA256 {t.Fatal(got,err)}
	db.row.raw=" "+string(raw)
	db.row.sha=renderDerivativeBytesSHA([]byte(db.row.raw))
	changed:=ref;changed.SHA256=db.row.sha
	if _,_,err:=ReadSignedDeliveryEnvelope(context.Background(),db,lease,changed);err==nil{t.Fatal("noncanonical envelope accepted")}
	db.row.raw=string(raw);db.row.sha=ref.SHA256
	bad:=lease;bad.Cursor=0
	calls:=db.calls
	if _,_,err:=ReadSignedDeliveryEnvelope(context.Background(),db,bad,ref);err==nil||db.calls!=calls{t.Fatal("incomplete lease reached SQL")}
}
