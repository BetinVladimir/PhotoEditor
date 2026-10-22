package v2workflow
import("context";"strings";"testing";"time";"github.com/google/uuid";"github.com/jackc/pgx/v5";"photoeditor/backend/internal/auth";"photoeditor/backend/internal/domain")
type authorityRow func(...any)error
func(r authorityRow)Scan(v ...any)error{return r(v...)}
type authorityTx struct{pgx.Tx;t *testing.T;reads int;revoked bool}
func(tx *authorityTx)QueryRow(_ context.Context,q string,_ ...any)pgx.Row{
 tx.reads++
 switch tx.reads{
 case 1:if !strings.Contains(q,"FROM photo.v2_workflows"){tx.t.Fatal("missing workflow scope query")};return authorityRow(func(v ...any)error{*v[0].(*uuid.UUID)=uuid.New();return nil})
 case 2:if !strings.Contains(q,"photo.authorize_tenant"){tx.t.Fatal("cached role reached project authorization")};return authorityRow(func(v ...any)error{if tx.revoked{return pgx.ErrNoRows};*v[0].(*domain.Role)=domain.Viewer;*v[1].(*int64)=2;return nil})
 case 3:if !strings.Contains(q,"FROM photo.projects"){tx.t.Fatal("missing fresh project authorization")};return authorityRow(func(v ...any)error{*v[0].(*uuid.UUID)=uuid.New();*v[1].(*uuid.UUID)=uuid.New();*v[2].(*string)="Project";*v[3].(*string)="";*v[4].(*string)="workspace";*v[5].(*bool)=true;*v[6].(*int64)=1;*v[7].(*time.Time)=time.Now();return nil})
 default:tx.t.Fatal("unexpected scope query");return authorityRow(func(...any)error{return pgx.ErrNoRows})
 }
}
func TestWorkflowAuthorizationRefreshesCachedAdminAfterScopeRead(t *testing.T){for _,revoked:=range []bool{false,true}{tx:=&authorityTx{t:t,revoked:revoked};_,err:=(Service{}).authorized(context.Background(),tx,auth.Identity{UserID:uuid.New(),SessionID:uuid.New()},uuid.New(),uuid.New(),domain.Admin,domain.Editor);if err==nil{t.Fatal("stale administrator authorized workflow")};want:=2;if tx.reads!=want{t.Fatal("unexpected authority chain",tx.reads)}}}
