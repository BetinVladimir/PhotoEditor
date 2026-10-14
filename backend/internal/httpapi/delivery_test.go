package httpapi

import (
 "fmt"
 "photoeditor/backend/internal/v2input"
	"context"
	"encoding/json"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"net/http"
	"net/http/httptest"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
	"strings"
	"testing"
)

type deliveryReaderStub struct {
 raw json.RawMessage
 cancel context.CancelFunc
	calls      int
	who        auth.Identity
	tenant, id uuid.UUID
	err        error
}

func deliveryHTTPDataFixture(tenant,id uuid.UUID,revision,state string,manifest any,published any)map[string]any{
 return map[string]any{"resource_type":"delivery_packages","id":id,"tenant_id":tenant,"project_id":uuid.New(),"created_at":"2026-10-11T11:00:00Z","revision":revision,"updated_at":"2026-10-11T12:00:00Z","workflow_id":uuid.New(),"snapshot_id":uuid.New(),"name":"Delivery package","policy_artifact_id":uuid.New(),"manifest_artifact_id":manifest,"published_at":published,"state":state}
}

func (s *deliveryReaderStub) Get(_ context.Context, who auth.Identity, tenant, id uuid.UUID) (json.RawMessage, error) {
	s.calls++
	s.who = who
	s.tenant = tenant
	s.id = id
	if s.cancel!=nil{s.cancel()}
 if s.raw!=nil{return s.raw,s.err}
 raw,_:=json.Marshal(map[string]any{"id":id,"revision":"1","document":nil,"data":deliveryHTTPDataFixture(tenant,id,"1","building",nil,nil)})
 return raw, s.err
}
func TestDeliveryRouteRequiresAuthentication(t *testing.T) {
	s := &Server{Auth: &auth.Service{Pepper: []byte("delivery-read")}}
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/tenants/"+uuid.NewString()+"/delivery-packages/"+uuid.NewString(), nil))
	if w.Code != 401 || w.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatal(w.Code, w.Header())
	}
}
func TestDeliveryDownloadIntentRouteRequiresAuthentication(t *testing.T) {
	s := &Server{Auth: &auth.Service{Pepper: []byte("delivery-download")}}
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, httptest.NewRequest("POST",
		"/api/v1/tenants/"+uuid.NewString()+"/delivery-packages/"+uuid.NewString()+"/download-intent",
		strings.NewReader(`{"kind":"manifest"}`)))
	if w.Code != 401 || w.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatal(w.Code, w.Header())
	}
}
func TestDeliveryListRouteRequiresAuthentication(t *testing.T){
 s:=&Server{Auth:&auth.Service{Pepper:[]byte("delivery-list")}}
 w:=httptest.NewRecorder()
 s.Routes().ServeHTTP(w,httptest.NewRequest("GET","/api/v1/tenants/"+uuid.NewString()+"/delivery-packages?project_id="+uuid.NewString(),nil))
 if w.Code!=401||w.Header().Get("Cache-Control")!="private, no-store"{t.Fatal(w.Code,w.Header())}
}
func TestDeliveryListQueryClosedScope(t *testing.T){
 project:=uuid.New()
 for _,tc:=range []struct{query string;valid bool;limit int}{
  {"project_id="+project.String(),true,50},
  {"project_id="+project.String()+"&limit=100&cursor=opaque",true,100},
  {"",false,0},
  {"project_id=bad",false,0},
  {"project_id="+project.String()+"&project_id="+project.String(),false,0},
  {"project_id="+project.String()+"&cursor=",false,0},
  {"project_id="+project.String()+"&limit=2&limit=3",false,0},
  {"project_id="+project.String()+"&unknown=1",false,0},
  {"project_id="+project.String()+"&cursor=%ZZ",false,0},
 }{
  request:=httptest.NewRequest("GET","/delivery-packages?"+tc.query,nil)
  id,n,_,err:=deliveryListQuery(request)
  if tc.valid&&(err!=nil||id!=project||n!=tc.limit)||!tc.valid&&err==nil{t.Fatal("delivery list query scope",tc.query,id,n,err)}
 }
}
func TestDeliveryHandlerScopeErrorsAndRawJSON(t *testing.T) {
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	tenant, id := uuid.New(), uuid.New()
	for _, tc := range []struct {
		name, path    string
		err           error
		status, calls int
	}{
		{"success", id.String(), nil, 200, 1},
		{"invalid", "bad", nil, 422, 0},
		{"zero", uuid.Nil.String(), nil, 422, 0},
		{"denied", id.String(), domain.NewError("NOT_FOUND", 404), 404, 1},
		{"unavailable", id.String(), domain.NewError("DELIVERY_UNAVAILABLE", 503), 503, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := &deliveryReaderStub{err: tc.err}
			router := chi.NewRouter()
			router.Get("/{id}", func(w http.ResponseWriter, r *http.Request) { serveDeliveryPackage(w, r, who, tenant, stub) })
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest("GET", "/"+tc.path, nil))
			if w.Code != tc.status || stub.calls != tc.calls {
				t.Fatal(w.Code, stub.calls, w.Body.String())
			}
			if tc.calls > 0 && (stub.who != who || stub.tenant != tenant || stub.id != id) {
				t.Fatal("scope changed")
			}
			if tc.status == 200 {
				var result map[string]json.RawMessage
				if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || result["data"] == nil {
					t.Fatal("double encoded resource", w.Body.String())
				}
			}
		})
	}
}

func TestDeliveryByWorkflowResolvesOnlyBoundPackage(t *testing.T){
 who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()};tenant,workflow:=uuid.New(),uuid.New()
 packageID:=uuid.NewSHA1(workflow,[]byte("delivery:package:1"))
 makeStub:=func(bound uuid.UUID)*deliveryReaderStub{
  data:=deliveryHTTPDataFixture(tenant,packageID,"1","building",nil,nil)
  data["workflow_id"]=bound
  raw,_:=json.Marshal(map[string]any{"id":packageID,"revision":"1","document":nil,"data":data})
  return &deliveryReaderStub{raw:raw}
 }
 for _,tc:=range []struct{name,path string;bound uuid.UUID;status,calls int}{
  {"owned",workflow.String(),workflow,200,1},
  {"foreign",workflow.String(),uuid.New(),503,1},
  {"invalid","bad",workflow,422,0},
 }{t.Run(tc.name,func(t *testing.T){
  stub:=makeStub(tc.bound);router:=chi.NewRouter()
  router.Get("/{workflow}",func(w http.ResponseWriter,r *http.Request){serveDeliveryByWorkflow(w,r,who,tenant,stub)})
  w:=httptest.NewRecorder();router.ServeHTTP(w,httptest.NewRequest("GET","/"+tc.path,nil))
  if w.Code!=tc.status||stub.calls!=tc.calls{t.Fatal(w.Code,stub.calls,w.Body.String())}
  if tc.calls>0&&(stub.id!=packageID||stub.tenant!=tenant||stub.who!=who){t.Fatal("workflow package scope changed")}
 })}
}
func TestDeliveryByRunDerivesScopedPackage(t *testing.T){
 who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()};tenant,run:=uuid.New(),uuid.New()
 workflow,err:=v2input.WorkflowID(run);if err!=nil{t.Fatal(err)}
 packageID:=uuid.NewSHA1(workflow,[]byte("delivery:package:1"))
 data:=deliveryHTTPDataFixture(tenant,packageID,"1","building",nil,nil);data["workflow_id"]=workflow
 raw,_:=json.Marshal(map[string]any{"id":packageID,"revision":"1","document":nil,"data":data})
 stub:=&deliveryReaderStub{raw:raw};router:=chi.NewRouter()
 router.Get("/{run}",func(w http.ResponseWriter,r *http.Request){serveDeliveryByRun(w,r,who,tenant,stub)})
 w:=httptest.NewRecorder();router.ServeHTTP(w,httptest.NewRequest("GET","/"+run.String(),nil))
 if w.Code!=200||stub.id!=packageID||stub.tenant!=tenant||stub.who!=who{t.Fatal("run lookup scope changed",w.Code,stub.id)}
 w=httptest.NewRecorder();router.ServeHTTP(w,httptest.NewRequest("GET","/bad",nil))
 if w.Code!=422||stub.calls!=1{t.Fatal("invalid run reached reader",w.Code,stub.calls)}
}

type deliveryPublisherStub struct {
	calls      int
	who        auth.Identity
	tenant, id uuid.UUID
	request    delivery.PublishRequest
	key        string
	err        error
}

func (s *deliveryPublisherStub) Publish(_ context.Context, who auth.Identity, tenant, id uuid.UUID, request delivery.PublishRequest, key string) (json.RawMessage, error) {
	s.calls++
	s.who = who
	s.tenant = tenant
	s.id = id
	s.request = request
	s.key = key
	manifest:=uuid.New()
 raw,_:=json.Marshal(map[string]any{"id":id,"revision":"2","document":map[string]any{"artifact_id":manifest,"sha256":strings.Repeat("a",64)},"data":deliveryHTTPDataFixture(tenant,id,"2","ready",manifest,"2026-10-11T12:00:00Z")})
 return raw,s.err
}
func TestPublishDeliveryHTTPDecodingAndIdentity(t *testing.T) {
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	tenant, id := uuid.New(), uuid.New()
	for _, tc := range []struct {
		body          string
		status, calls int
	}{
		{`{"expected_revision":"1"}`, 200, 1},
		{`{"expected_revision":1}`, 422, 0},
		{`{"expected_revision":""}`, 422, 0},
		{`{"expected_revision":"0"}`, 422, 0},
		{`{"expected_revision":"01"}`, 422, 0},
		{`{"expected_revision":"+1"}`, 422, 0},
		{`{"expected_revision":"-1"}`, 422, 0},
		{`{"expected_revision":"9007199254740991"}`, 422, 0},
		{`{"expected_revision":"9223372036854775808"}`, 422, 0},

		{`{"expected_revision":"1","unknown":true}`, 422, 0},
		{`{"expected_revision":"1","expected_revision":"2"}`, 400, 0},
	} {
		stub := &deliveryPublisherStub{}
		router := chi.NewRouter()
		router.Post("/{id}", func(w http.ResponseWriter, r *http.Request) { servePublishDelivery(w, r, who, tenant, stub) })
		req := httptest.NewRequest("POST", "/"+id.String(), strings.NewReader(tc.body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", "publish-key")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != tc.status || stub.calls != tc.calls {
			t.Fatal(w.Code, stub.calls, w.Body.String())
		}
		if stub.calls > 0 && (stub.who != who || stub.tenant != tenant || stub.id != id || stub.key != "publish-key" || stub.request.ExpectedRevision != "1") {
			t.Fatal("publication scope changed")
		}
	}
}

type deliveryReceiptPublisherStub struct {
	deliveryPublisherStub
	replayed bool
}

func (s *deliveryReceiptPublisherStub) PublishWithReceipt(ctx context.Context, who auth.Identity, tenant, id uuid.UUID, request delivery.PublishRequest, key string) (idempotency.Response, error) {
	body, err := s.deliveryPublisherStub.Publish(ctx, who, tenant, id, request, key)
	return idempotency.Response{Status: 200, Body: body, Replayed: s.replayed}, err
}
func TestPublishDeliveryHTTPReplayHeader(t *testing.T) {
	for _, replayed := range []bool{false, true} {
		for _, fail := range []bool{false, true} {
			stub := &deliveryReceiptPublisherStub{replayed: replayed}
			if fail {
				stub.err = domain.NewError("IDEMPOTENCY_CONFLICT", 409)
			}
			router := chi.NewRouter()
			router.Post("/{id}", func(w http.ResponseWriter, r *http.Request) {
				servePublishDelivery(w, r, auth.Identity{}, uuid.New(), stub)
			})
			req := httptest.NewRequest("POST", "/"+uuid.New().String(), strings.NewReader(`{"expected_revision":"1"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Idempotency-Key", "publish-key")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			expectedStatus := 200
			if fail {
				expectedStatus = 409
			}
			expectedHeader := ""
			if replayed && !fail {
				expectedHeader = "true"
			}
			if w.Code != expectedStatus || w.Header().Get("Idempotency-Replayed") != expectedHeader || stub.calls != 1 {
				t.Fatal(w.Code, w.Header(), stub.calls)
			}
		}
	}
}

type deliveryItemsReaderStub struct {
	calls      int
	who        auth.Identity
	tenant, id uuid.UUID
	query      delivery.ItemsQuery
	err        error
}

func (s *deliveryItemsReaderStub) Items(_ context.Context, who auth.Identity, tenant, id uuid.UUID, q delivery.ItemsQuery) (json.RawMessage, error) {
	s.calls++
	s.who = who
	s.tenant = tenant
	s.id = id
	s.query = q
	return json.RawMessage(fmt.Sprintf(`{"package_id":%q,"revision":%q,"manifest":null,"items":[],"next":null}`,id.String(),q.Revision)), s.err
}
func TestDeliveryItemsHTTPQueryBoundary(t *testing.T) {
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	tenant, id, preset := uuid.New(), uuid.New(), uuid.New()
	for _, tc := range []struct {
		query         string
		status, calls int
	}{
		{"revision=1", 200, 1},
		{"revision=2&after_ordinal=0&after_preset=" + preset.String() + "&limit=200", 200, 1},
		{"", 422, 0}, {"revision=01", 422, 0}, {"revision=1&revision=2", 422, 0},
		{"revision=1&unknown=true", 422, 0}, {"revision=1&limit=201", 422, 0},
		{"revision=1&limit=01", 422, 0}, {"revision=1&after_ordinal=0", 422, 0},
		{"revision=1&after_preset=" + preset.String(), 422, 0},
		{"revision=1&after_ordinal=-1&after_preset=" + preset.String(), 422, 0},
		{"revision=1&after_ordinal=5000&after_preset=" + preset.String(), 422, 0},
		{"revision=1&after_ordinal=0&after_preset=bad", 422, 0},
		{"revision=1&limit=%zz", 422, 0},
	} {
		t.Run(tc.query, func(t *testing.T) {
			stub := &deliveryItemsReaderStub{}
			router := chi.NewRouter()
			router.Get("/{id}/items", func(w http.ResponseWriter, r *http.Request) { serveDeliveryItems(w, r, who, tenant, stub) })
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest("GET", "/"+id.String()+"/items?"+tc.query, nil))
			if w.Code != tc.status || stub.calls != tc.calls {
				t.Fatal(w.Code, stub.calls, w.Body.String())
			}
			if stub.calls > 0 && (stub.who != who || stub.tenant != tenant || stub.id != id) {
				t.Fatal("scope changed")
			}
            if tc.status==200&&w.Header().Get("Cache-Control")!="private, no-store"{t.Fatal("delivery items cacheable")}
			if tc.query == "revision=1" && (stub.query.AfterOrdinal != -1 || stub.query.AfterPreset != uuid.Nil || stub.query.Limit != 100) {
				t.Fatal("wrong defaults", stub.query)
			}
		})
	}
}

func TestDeliveryHTTPRejectsMalformedReaderResource(t *testing.T){
 who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()};tid,id:=uuid.New(),uuid.New()
 for _,raw:=range []string{`null`,`[]`,`{"id":"foreign"}`,`{"id":"private","bucket":"secret"}`} {
  stub:=&deliveryReaderStub{raw:json.RawMessage(raw)};router:=chi.NewRouter()
  router.Get("/{id}",func(w http.ResponseWriter,r *http.Request){serveDeliveryPackage(w,r,who,tid,stub)})
  w:=httptest.NewRecorder();router.ServeHTTP(w,httptest.NewRequest("GET","/"+id.String(),nil))
  if w.Code!=503||strings.Contains(w.Body.String(),"secret"){t.Fatal("malformed reader leaked",w.Code,w.Body.String())}
 }
}
func TestDeliveryHTTPDoesNotWriteLateCancelledResource(t *testing.T){
 who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()};tid,id:=uuid.New(),uuid.New();ctx,cancel:=context.WithCancel(context.Background());defer cancel()
 stub:=&deliveryReaderStub{cancel:cancel};router:=chi.NewRouter()
 router.Get("/{id}",func(w http.ResponseWriter,r *http.Request){serveDeliveryPackage(w,r,who,tid,stub)})
 w:=httptest.NewRecorder();router.ServeHTTP(w,httptest.NewRequest("GET","/"+id.String(),nil).WithContext(ctx))
 if w.Body.Len()!=0{t.Fatal("cancelled response written",w.Body.String())}
}

type malformedDeliveryReceipt struct{deliveryPublisherStub;mode string}
func(s *malformedDeliveryReceipt)PublishWithReceipt(ctx context.Context,who auth.Identity,tid,id uuid.UUID,request delivery.PublishRequest,key string)(idempotency.Response,error){
 body,err:=s.Publish(ctx,who,tid,id,request,key);response:=idempotency.Response{Status:200,Body:body,ResourceID:&id,Replayed:true}
 switch s.mode{case "status":response.Status=202;case "resource":other:=uuid.New();response.ResourceID=&other;case "body":response.Body=json.RawMessage(`{"bucket":"secret"}`)}
 return response,err
}
func TestDeliveryPublishHTTPValidatesReceiptBeforeReplayHeader(t *testing.T){
 who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()};tid,id:=uuid.New(),uuid.New()
 for _,mode:=range []string{"valid","status","resource","body"}{t.Run(mode,func(t *testing.T){
  provider:=&malformedDeliveryReceipt{mode:mode};router:=chi.NewRouter()
  router.Post("/{id}",func(w http.ResponseWriter,r *http.Request){servePublishDelivery(w,r,who,tid,provider)})
  req:=httptest.NewRequest("POST","/"+id.String(),strings.NewReader(`{"expected_revision":"1"}`));req.Header.Set("Content-Type","application/json");req.Header.Set("Idempotency-Key","key")
  w:=httptest.NewRecorder();router.ServeHTTP(w,req)
  if mode=="valid"{if w.Code!=200||w.Header().Get("Idempotency-Replayed")!="true"{t.Fatal(w.Code,w.Header())}}else if w.Code!=503||w.Header().Get("Idempotency-Replayed")!=""||strings.Contains(w.Body.String(),"secret"){t.Fatal("invalid receipt emitted",w.Code,w.Body.String())}
 })}
}


type deliveryInspectionStub struct{cancel context.CancelFunc}
func(s deliveryInspectionStub)Inspect(_ context.Context,_ auth.Identity,_ uuid.UUID,id uuid.UUID,revision string)(delivery.Inspection,error){
 if s.cancel!=nil{s.cancel()};return delivery.Inspection{PackageID:id,Revision:revision,Manifest:v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("a",64)},SlotCount:1,ReadyCount:1},nil
}
func TestDeliveryInspectionCacheAndCancellation(t *testing.T){
 for _,cancelled:=range []bool{false,true}{
  ctx,cancel:=context.WithCancel(context.Background())
  who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()};tenant,id:=uuid.New(),uuid.New()
  stub:=deliveryInspectionStub{};if cancelled{stub.cancel=cancel}
  router:=chi.NewRouter();router.Get("/{id}",func(w http.ResponseWriter,r *http.Request){serveDeliveryInspection(w,r,who,tenant,stub)})
  w:=httptest.NewRecorder();router.ServeHTTP(w,httptest.NewRequest("GET","/"+id.String()+"?revision=1",nil).WithContext(ctx));cancel()
  if cancelled{if w.Body.Len()!=0{t.Fatal("late cancelled inspection written")}}else if w.Code!=200||w.Header().Get("Cache-Control")!="private, no-store"{t.Fatal("inspection cache policy",w.Code,w.Header())}
 }
}
