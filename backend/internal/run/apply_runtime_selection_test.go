package run
import("context";"errors";"strings";"testing";"github.com/google/uuid";"github.com/jackc/pgx/v5";"photoeditor/backend/internal/domain")
type applySelectionRow struct{err error}
func(r applySelectionRow)Scan(values ...any)error{if r.err!=nil{return r.err};*(values[0].(*string))=strings.Repeat("a",64);*(values[1].(*string))="mps";return nil}
type applySelectionQuery struct{calls int;sql string;err error}
func(q *applySelectionQuery)QueryRow(_ context.Context,sql string,_ ...any)pgx.Row{q.calls++;q.sql=sql;return applySelectionRow{q.err}}
func TestApplyRuntimeSelectionUsesSameQualifiedMetalAlternative(t *testing.T){
 var needs styleExecutionNeeds;needs.Mode="fixed";needs.Local.Enabled=true;needs.Models=[]styleModelReference{{personModelID,personWeightID}}
 q:=&applySelectionQuery{};out,e:=selectApplyRuntimes(context.Background(),q,uuid.New(),needs,false);if e!=nil||!out.PersonRequired||out.PersonBackend!="mps"||q.calls!=1{t.Fatal(out,e,q.calls)}
 for _,clause:=range []string{"qualified_operations->'mask.person'","last_seen>clock_timestamp()-interval '30 seconds'","ARRAY['cuda','mps','cpu']"}{if !strings.Contains(q.sql,clause){t.Fatal("qualification clause missing",clause)}}
 q.err=pgx.ErrNoRows;_,e=selectApplyRuntimes(context.Background(),q,uuid.New(),needs,false);if domain.AsError(e).Code!="PERSON_MASK_CAPABILITY_UNAVAILABLE"{t.Fatal(e)}
 q.err=errors.New("database offline");_,e=selectApplyRuntimes(context.Background(),q,uuid.New(),needs,false);if e!=q.err{t.Fatal("database error hidden",e)}
}
func TestApplyFixedStyleDoesNotInventGPURequirement(t *testing.T){var needs styleExecutionNeeds;needs.Mode="fixed";q:=&applySelectionQuery{};out,e:=selectApplyRuntimes(context.Background(),q,uuid.New(),needs,false);if e!=nil||out.PersonRequired||q.calls!=0{t.Fatal(out,e,q.calls)}}
func TestApplyCapabilityRevisionCanonicalSafeBounds(t *testing.T){for _,raw:=range []string{"","0","01","-1","1e2","9007199254740992","9223372036854775808"}{if _,e:=parseApplyCapabilityRevision(raw);e==nil{t.Fatal(raw)}};if n,e:=parseApplyCapabilityRevision("9007199254740991");e!=nil||n!=9007199254740991{t.Fatal(n,e)}}
