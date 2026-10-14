package v2optical

import (
 "bytes"
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
func TestNegativeReplacementTopologyDiamondAndFrozenOwnership(t *testing.T){
 origin,left,right,join,retained:=uuid.New(),uuid.New(),uuid.New(),uuid.New(),uuid.New()
 run,other:=uuid.New(),uuid.New();tenant,project,review:=uuid.New(),uuid.New(),uuid.New()
 inventory:=[]NegativeDependencyTask{{TaskID:join,RunID:other,Parents:[]uuid.UUID{right,left,retained}},{TaskID:left,RunID:run,Parents:[]uuid.UUID{origin}},{TaskID:origin,RunID:run,Parents:[]uuid.UUID{}},{TaskID:retained,RunID:other,Parents:[]uuid.UUID{}},{TaskID:right,RunID:other,Parents:[]uuid.UUID{origin}}}
 plan,err:=PlanNegativeInvalidation(context.Background(),origin,run,"base",inventory,10,10);if err!=nil {t.Fatal(err)}
 base:=v2input.Ref{ArtifactID:uuid.New(),SHA256:"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"};hash:=base.SHA256
 compile:=func(input []NegativeDependencyTask,reviewID uuid.UUID)(NegativeReplacementTopology,error){return CompileNegativeReplacementTopology(context.Background(),tenant,project,reviewID,hash,plan,input,&base,10,10)}
 out,err:=compile(inventory,review);if err!=nil||len(out.Nodes)!=3 {t.Fatal("compile",err)}
 if out.SchemaVersion!=2 {t.Fatal("replacement topology version")}
 legacy:=out;legacy.SchemaVersion=1
 if _,err:=CompileNegativeReplacementFrontier(context.Background(),legacy,plan,inventory,nil);err==nil {t.Fatal("legacy grouped topology accepted")}
 positions:=map[uuid.UUID]int{};nodes:=map[uuid.UUID]NegativeReplacementNode{}
 for i,n:=range out.Nodes {positions[n.OriginalTask]=i;nodes[n.OriginalTask]=n;if n.Task==n.OriginalTask||n.Run==n.OriginalRun {t.Fatal("old identity reused")}}
 if positions[join]<=positions[left]||positions[join]<=positions[right]||nodes[right].Run==nodes[join].Run||nodes[left].Run==nodes[right].Run {t.Fatal("topology/per-node run isolation")}
 if nodes[left].Parents[0].Kind!="selected_base"||nodes[left].Parents[0].Task!=uuid.Nil {t.Fatal("declined source retained")}
 for _,p:=range nodes[join].Parents {if p.Original==retained {if p.Kind!="retained"||p.Task!=retained {t.Fatal("unaffected dependency changed")}} else if p.Kind!="replacement"||p.Task!=nodes[p.Original].Task {t.Fatal("replacement edge mismatch")}}
 shuffled:=append([]NegativeDependencyTask(nil),inventory...);for i,j:=0,len(shuffled)-1;i<j;i,j=i+1,j-1 {shuffled[i],shuffled[j]=shuffled[j],shuffled[i]}
 again,err:=compile(shuffled,review);a,_:=canonical.Marshal(out);b,_:=canonical.Marshal(again);if err!=nil||!bytes.Equal(a,b) {t.Fatal("nondeterministic topology",err)}
 changed,err:=compile(inventory,uuid.New());if err!=nil||changed.Nodes[0].Task==out.Nodes[0].Task {t.Fatal("decision namespace reused",err)}
 saved:=*out.SelectedBase;base.SHA256="bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb";inventory[0].Parents[0]=uuid.New()
 if *out.SelectedBase!=saved||nodes[join].Parents[0].Original==inventory[0].Parents[0] {t.Fatal("caller mutation leaked")}
}
func TestNegativeReplacementSkipAndPlanTampering(t *testing.T){
 origin,child,run:=uuid.New(),uuid.New(),uuid.New();tenant,project,review:=uuid.New(),uuid.New(),uuid.New()
 inventory:=[]NegativeDependencyTask{{TaskID:origin,RunID:run,Parents:[]uuid.UUID{}},{TaskID:child,RunID:run,Parents:[]uuid.UUID{origin}}}
 plan,_:=PlanNegativeInvalidation(context.Background(),origin,run,"skip",inventory,10,10)
 hash:="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
 out,err:=CompileNegativeReplacementTopology(context.Background(),tenant,project,review,hash,plan,inventory,nil,10,10)
 if err!=nil||len(out.Nodes)!=0||len(out.OmittedTasks)!=1||out.OmittedTasks[0]!=child||out.SelectedBase!=nil {t.Fatal("skip manufactured content",err)}
 base:=v2input.Ref{ArtifactID:uuid.New(),SHA256:hash}
 if _,err:=CompileNegativeReplacementTopology(context.Background(),tenant,project,review,hash,plan,inventory,&base,10,10);err==nil {t.Fatal("skip accepted base")}
 plan.AffectedTasks=[]uuid.UUID{}
 if _,err:=CompileNegativeReplacementTopology(context.Background(),tenant,project,review,hash,plan,inventory,nil,10,10);err==nil {t.Fatal("incomplete plan accepted")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=CompileNegativeReplacementTopology(ctx,tenant,project,review,hash,plan,inventory,nil,10,10);err!=context.Canceled {t.Fatal("cancel ignored",err)}
}
