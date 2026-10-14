package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
	"time"
)

type deliveryAssemblyTransportTx struct {
	pgx.Tx
	ready, refuse bool
	calls         []string
}

func (tx *deliveryAssemblyTransportTx) QueryRow(_ context.Context, q string, args ...any) pgx.Row {
	tx.calls = append(tx.calls, q)
	switch {
	case strings.Contains(q, "assembly_transport_ready"):
		return deliveryActivationRow{tx.ready}
	case strings.Contains(q, "create_v30_delivery_assembly_task"):
		return deliveryActivationRow{!tx.refuse}
	case strings.Contains(q, "register_v2_task_input_batch"):
		var entries []v2input.Entry
		if json.Unmarshal(args[4].(json.RawMessage), &entries) != nil {
			panic("invalid entries")
		}
		return deliveryActivationRow{len(entries)}
	case strings.Contains(q, "seal_v2_task_inputs"):
		tx.ready = true
		return deliveryActivationRow{true}
	default:
		panic("unexpected assembly SQL")
	}
}
func TestFrozenAssemblyRegistersOwnedInventoryAndRetriesReceipt(t *testing.T) {
	plan, parent, _, _, _ := deliveryExportPlanFixture(t, nil)
	workflow, _ := v2input.WorkflowID(parent)
	lease := delivery.ExpansionLease{Expansion: uuid.New(), Token: uuid.New(), Tenant: uuid.New(), Package: uuid.New(), Run: parent, Workflow: workflow, ParentTask: plan.TaskID(), Until: time.Now().Add(time.Minute), SlotCount: 1, Cursor: 1}
	root := v2input.Ref{ArtifactID: uuid.New(), SHA256: downloadDigest("root")}
	roots := []v2input.Entry{{Ref: root, Contract: "DeliveryAssemblyInput"}, {Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: downloadDigest("shard")}, Contract: "DeliverySlotShard"}, {Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: downloadDigest("config")}, Contract: "ExportPresetConfig"}}
	sha, err := canonical.Hash(v2input.DeliveryAssembleParams{SchemaVersion: 1, Assembly: root})
	if err != nil {
		t.Fatal(err)
	}
	profile := json.RawMessage(`{"backend":"cpu","renderer_version":"v2.2","resource_estimate":{"ram_bytes":134217728,"vram_bytes":0,"scratch_bytes":8388608,"output_bytes":8388608,"pixel_count":0}}`)
	frozen, err := FreezeDeliveryAssemblyTask(lease, plan, root, v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}, roots, roots, profile)
	if err != nil {
		t.Fatal(err)
	}
	roots[0] = v2input.Entry{}
	profile[0] = '!'
	graph, err := frozen.PublishInputs(context.Background(), deliveryTransportPublisher{})
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"fresh", "ready", "create_error", "renew_error", "register_error", "publish_error"} {
		t.Run("coordinator_"+scenario, func(t *testing.T) {
			store := &assemblyRegistrationStore{ready: scenario == "ready", fail: scenario}
			publisher := &assemblyRegistrationPublisher{fail: scenario == "publish_error"}
			id, err := frozen.RegisterPreparedAssembly(context.Background(), store, publisher, lease, plan)
			if scenario == "fresh" {
				if err != nil || id == uuid.Nil || strings.Join(store.steps, ",") != "registered,create,renew,renew,register" || publisher.calls == 0 {
					t.Fatal("wrong admission sequence", err, store.steps)
				}
			} else if scenario == "ready" {
				if err != nil || id == uuid.Nil || len(store.steps) != 1 || publisher.calls != 0 {
					t.Fatal("ready retry republished", err, store.steps)
				}
			} else if err == nil || id != uuid.Nil {
				t.Fatal("failed registration returned task", scenario)
			}
		})
	}

 for _,mode:=range []string{"tenant","package","token","expiry","cursor","foreign-parent"}{t.Run("coordinator_invalid_"+mode,func(t *testing.T){
  invalid:=lease
  switch mode{
  case "tenant":invalid.Tenant=uuid.Nil
  case "package":invalid.Package=uuid.Nil
  case "token":invalid.Token=uuid.Nil
  case "expiry":invalid.Until=time.Time{}
  case "cursor":invalid.Cursor=invalid.SlotCount+1
  case "foreign-parent":invalid.ParentTask=uuid.New()
  }
  store:=&assemblyRegistrationStore{}
  publisher:=&assemblyRegistrationPublisher{}
  id,err:=frozen.RegisterPreparedAssembly(context.Background(),store,publisher,invalid,plan)
  if err==nil||id!=uuid.Nil||len(store.steps)!=0||publisher.calls!=0{t.Fatal("invalid lease caused side effects",mode,id,err,store.steps,publisher.calls)}
 })}

 for _,at:=range []int{1,2,3,4,5}{t.Run("coordinator_cancel_"+string(rune('0'+at)),func(t *testing.T){
  ctx,cancel:=context.WithCancel(context.Background());defer cancel()
  store:=&assemblyRegistrationStore{cancel:cancel,cancelAt:at}
  publisher:=&assemblyRegistrationPublisher{}
  id,err:=frozen.RegisterPreparedAssembly(ctx,store,publisher,lease,plan)
  if !errors.Is(err,context.Canceled)||id!=uuid.Nil||len(store.steps)!=at{t.Fatal("cancelled coordinator advanced",at,id,err,store.steps)}
  if at<=3&&publisher.calls!=0{t.Fatal("cancelled coordinator published",at,publisher.calls)}
 })}
	graph.Open = func(context.Context) (v2input.Cursor, error) { panic("caller cursor used") }
	graph.Direct = nil
	for _, scenario := range []string{"fresh", "ready", "refused", "foreign", "cancel"} {
		t.Run(scenario, func(t *testing.T) {
			tx := &deliveryAssemblyTransportTx{ready: scenario == "ready", refuse: scenario == "refused"}
			candidate := graph
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if scenario == "foreign" {
				candidate.ID = uuid.New()
			}
			if scenario == "cancel" {
				cancel()
			}
			err := frozen.RegisterInputsInTransaction(ctx, tx, lease, plan, candidate)
			switch scenario {
			case "fresh":
				if err != nil || len(tx.calls) != 5 || !tx.ready {
					t.Fatal("registration incomplete", err, tx.calls)
				}
			case "ready":
				if err != nil || len(tx.calls) != 1 {
					t.Fatal("sealed transport registered twice", err, tx.calls)
				}
			case "refused":
				if err == nil || len(tx.calls) != 2 {
					t.Fatal("refused shell registered", tx.calls)
				}
			default:
				if err == nil || len(tx.calls) != 0 {
					t.Fatal("invalid preflight reached SQL", tx.calls)
				}
			}
		})
	}
}

type assemblyRegistrationStore struct {
	ready bool
	fail  string
	steps []string
 cancel context.CancelFunc
 cancelAt int
}
func(s *assemblyRegistrationStore) record(step string){
 s.steps=append(s.steps,step)
 if s.cancel!=nil&&len(s.steps)==s.cancelAt{s.cancel()}
}

func (s *assemblyRegistrationStore) Registered(context.Context, FrozenDeliveryAssemblyTask, delivery.ExpansionLease, delivery.PreparePlan) (bool, error) {
	s.record("registered")
	return s.ready, nil
}
func (s *assemblyRegistrationStore) Create(context.Context, FrozenDeliveryAssemblyTask, delivery.ExpansionLease, delivery.PreparePlan) error {
	s.record("create")
	if s.fail == "create_error" {
		return errors.New("create failed")
	}
	return nil
}
func (s *assemblyRegistrationStore) Renew(context.Context, delivery.ExpansionLease) error {
	s.record("renew")
	if s.fail == "renew_error" {
		return errors.New("lease lost")
	}
	return nil
}
func (s *assemblyRegistrationStore) Register(_ context.Context, _ FrozenDeliveryAssemblyTask, _ delivery.ExpansionLease, _ delivery.PreparePlan, g v2input.GraphTask) error {
	s.record("register")
	if g.ID == uuid.Nil || s.fail == "register_error" {
		return errors.New("registration failed")
	}
	return nil
}

type assemblyRegistrationPublisher struct {
	calls int
	fail  bool
}

func (p *assemblyRegistrationPublisher) Publish(ctx context.Context, task uuid.UUID, ordinal int, contract string, body []byte) (v2input.Ref, error) {
	p.calls++
	if p.fail {
		return v2input.Ref{}, errors.New("object unavailable")
	}
	return (deliveryTransportPublisher{}).Publish(ctx, task, ordinal, contract, body)
}
