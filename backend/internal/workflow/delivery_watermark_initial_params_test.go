package workflow
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/delivery"
)
func TestWatermarkInitialParamsAdapterCommitsAndRestrictsQueries(t *testing.T){
 tenant:=uuid.New();tx:=&watermarkParamsTx{row:deliveryParamsRow{outcome:"created",id:uuid.New(),token:uuid.New()}};pool:=&watermarkParamsPool{tx:tx}
 lease:=delivery.ExpansionLease{Tenant:tenant,Run:uuid.New(),Expansion:uuid.New(),Token:uuid.New(),SlotCount:1,Cursor:0}
 repository:=deliveryParamsRepository{db:watermarkInitialParamsQueries{db:pool,tenant:tenant,image:[]byte(`{}`)},bucket:"photos",lease:lease,owner:uuid.New(),params:[]byte(`{"image":{}}`)}
 if _,err:=repository.Reserve(context.Background(),repository.scope(),renderDerivativeBytesSHA(repository.params),int64(len(repository.params)));err!=nil{t.Fatal(err)}
 if tx.query!=`SELECT outcome,artifact_id,token,ready FROM photo.reserve_v30_watermark_initial_params($1,$2,$3,$4,$5,$6,$7,$8,$9)`||len(tx.events)!=4||tx.events[2]!="commit"{t.Fatal("transaction/query isolation",tx.events,tx.query)}
 adapter:=watermarkInitialParamsQueries{db:pool,tenant:tenant,image:[]byte(`{}`)};before:=pool.begins;var accepted bool
 if err:=adapter.QueryRow(context.Background(),"SELECT true",tenant).Scan(&accepted);err==nil||pool.begins!=before{t.Fatal("arbitrary SQL accepted")}
 foreign:=artifactjson.Scope{Tenant:uuid.New()}
 if _,err:=repository.Reserve(context.Background(),foreign,"invalid",0);err==nil||pool.begins!=before{t.Fatal("foreign scope accepted")}
}

func TestWatermarkPresentationParamsAdapterUsesDedicatedScope(t *testing.T){
 tenant:=uuid.New();tx:=&watermarkParamsTx{row:deliveryParamsRow{outcome:"created",id:uuid.New(),token:uuid.New()}};pool:=&watermarkParamsPool{tx:tx}
 lease:=delivery.ExpansionLease{Tenant:tenant,Run:uuid.New(),Expansion:uuid.New(),Token:uuid.New(),SlotCount:1,Cursor:0}
 repository:=deliveryParamsRepository{db:watermarkInitialParamsQueries{db:pool,tenant:tenant,image:[]byte(`{}`),afterPresentation:true},bucket:"photos",lease:lease,owner:uuid.New(),params:[]byte(`{"image":{}}`)}
 if _,err:=repository.Reserve(context.Background(),repository.scope(),renderDerivativeBytesSHA(repository.params),int64(len(repository.params)));err!=nil{t.Fatal(err)}
 if tx.query!=`SELECT outcome,artifact_id,token,ready FROM photo.reserve_v30_watermark_presentation_params($1,$2,$3,$4,$5,$6,$7,$8,$9)`||tx.events[2]!="commit"{t.Fatal("presentation publication scope",tx.query,tx.events)}
}
