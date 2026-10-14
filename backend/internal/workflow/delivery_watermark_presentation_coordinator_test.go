package workflow
import (
 "context"
 "errors"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/delivery"
 "reflect"
 "testing"
)
func exerciseWatermarkPresentationCoordinator(t *testing.T, frozen FrozenDeliveryWatermarkPrefixTask) {
	t.Helper()
	for _, scenario := range []string{"fresh", "activated", "registered", "create", "publish", "commit"} {
		calls := []string{}
		store := continuationCoordinatorStore{calls: &calls, activated: scenario == "activated", registered: scenario == "registered", fail: scenario}
		if scenario != "create" && scenario != "commit" {
			store.fail = ""
		}
		publisher := deliveryCoordinatorPublisher{calls: &calls, fail: scenario == "publish"}
		recovered, err := frozen.admitWatermarkPresentation(context.Background(), store, publisher)
		switch scenario {
		case "activated":
			if err != nil || !recovered || !reflect.DeepEqual(calls, []string{"activated"}) {
				t.Fatal(scenario, calls, err)
			}
		case "registered":
			if err != nil || recovered || !reflect.DeepEqual(calls, []string{"activated", "registered", "commit"}) {
				t.Fatal(scenario, calls, err)
			}
		case "fresh":
			if err != nil || recovered || len(calls) < 5 || !reflect.DeepEqual(calls[:3], []string{"activated", "registered", "create"}) || calls[len(calls)-1] != "commit" {
				t.Fatal(scenario, calls, err)
			}
			for _, call := range calls[3 : len(calls)-1] {
				if call != "publish" {
					t.Fatal("IO outside phase", calls)
				}
			}
		default:
			if err == nil || recovered {
				t.Fatal("failure accepted", scenario)
			}
			if scenario == "create" && !reflect.DeepEqual(calls, []string{"activated", "registered", "create"}) {
				t.Fatal(calls)
			}
			if scenario == "publish" && calls[len(calls)-1] != "publish" {
				t.Fatal(calls)
			}
			if scenario == "commit" && calls[len(calls)-1] != "commit" {
				t.Fatal(calls)
			}
		}
	}
}

func TestWatermarkPresentationCoordinatorTransactionCommitsTenantScope(t *testing.T){
 tenant:=uuid.New();tx:=&watermarkParamsTx{};pool:=&watermarkParamsPool{tx:tx}
 store:=sqlWatermarkPresentationAdmission{db:pool,lease:delivery.ExpansionLease{Tenant:tenant}}
 if err:=store.transaction(context.Background(),func(ctx context.Context,got pgx.Tx)error{
  if got!=tx||ctx.Err()!=nil||!reflect.DeepEqual(tx.events,[]string{"tenant"}){t.Fatal("transaction scope",tx.events)}
  return nil
 });err!=nil{t.Fatal(err)}
 if !reflect.DeepEqual(tx.events,[]string{"tenant","commit","rollback"}){t.Fatal("commit boundary",tx.events)}
 tx.events=nil
 failure:=errors.New("registration refused")
 if err:=store.transaction(context.Background(),func(context.Context,pgx.Tx)error{return failure});!errors.Is(err,failure){t.Fatal(err)}
 if !reflect.DeepEqual(tx.events,[]string{"tenant","rollback"}){t.Fatal("failed callback committed",tx.events)}
}
