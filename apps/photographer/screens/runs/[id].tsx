import {useLocale} from '../../src/locale-context';
import {Button,ActivityIndicator,Text} from 'react-native-paper';
import {useWorkspaceAccess} from '../../src/workspace-access-context';
import {readRunStatusResponse} from '../../src/run-status-response';
import {readDeadline} from "../../src/read-deadline";
import {ReadResourceState} from "../../src/read-resource-state";
import {LocalPortraitRunDiscovery} from "../../src/local-portrait-run-discovery";
import {MultiRunReviewView} from "../../src/multi-run-review-view";
import {LocalPortraitRunResult} from "../../src/local-portrait-run-result";
import {runRestartNotice} from "../../src/run-restart-notice";
import {useReviewForeground} from '../../src/use-review-foreground';
import {ContactSheetRunOutputs} from "../../src/contact-sheet-run-outputs";
import {admitRunMutation} from "../../src/run-mutation-admission";
import {SemanticRunLink} from "../../src/semantic-run-link";
import {RelightRunReviews} from "../../src/relight-run-reviews";
import {RunReviews} from "../../src/run-reviews";
import {DeliveryRunLink} from "../../src/delivery-run-link";
import {DeliveryRunDiscovery} from "../../src/delivery-run-discovery";
import {OwnerAPI} from "../../src/owner-api";
import {RunRetryAttemptStore,type RunRetryAttempt} from "../../src/run-retry-attempt-store";
import {RunCancelAttemptStore,type RunCancelAttempt} from "../../src/run-cancel-attempt-store";
import {recoverPendingRunMutations,runMutationDefinitelyRejected} from "../../src/run-mutation-recovery";
import AsyncStorage from "@react-native-async-storage/async-storage";
import {APIError} from '@photoeditor/api-client';
import {watchRun} from "../../src/run-watch";
import{useEffect,useMemo,useState,useRef}from"react";import{router,useLocalSearchParams}from'../../src/localized-router';import {SafeAreaView,ScrollView,StyleSheet,View} from 'react-native';import{useAuth}from"../../src/auth-context";import{catalogStyle as s}from"../../src/catalog-style";
type RunStatus={id:string;kind:string;status:string;revision:string;counts:{total:number;succeeded:number;failed:number;skipped:number;review:number};output_collection_id?:string;stage:string;progress:number;errors:string[]};const terminal=new Set(["completed","completed_with_errors","failed","cancelled"]);
function RunScope(){
 const __r2Value=useLocale().value;const __r2T=useLocale().t;

 const __uiMessage=useLocale().message;

 const __uiT=useLocale().t;
const{id,tenant,semanticWorkflow,relightWorkflow,relightAsset,relightVersion,localPortraitWorkflow,deliveryWorkflow,deliveryProject}=useLocalSearchParams<{id:string;tenant:string;semanticWorkflow?:string;relightWorkflow?:string;relightAsset?:string;relightVersion?:string;localPortraitWorkflow?:string;deliveryWorkflow?:string;deliveryProject?:string}>();const auth=useAuth();const workspace=useWorkspaceAccess(tenant||'');const canEdit=workspace?.canEdit===true;const client=useMemo(()=>auth.client(),[auth]);const api=useMemo(()=>new OwnerAPI(client,tenant||""),[client,tenant]);const[run,setRun]=useState<RunStatus>();const monitor=useRef<ReturnType<typeof watchRun>|undefined>(undefined);const lifecycle=useRef<AbortController|undefined>(undefined);const[busy,setBusy]=useState(true);const[reload,setReload]=useState(0);const mutating=useRef(false);const mutationFlight=useRef<AbortController|null>(null);const[confirming,setConfirming]=useState(false);const[error,setError]=useState("");const root=`/api/v1/tenants/${encodeURIComponent(tenant||"")}/runs/${encodeURIComponent(id||"")}`;useEffect(()=>{
 const controller=new AbortController(),recoveryController=new AbortController();
 const stopRecovery=()=>recoveryController.abort();
 controller.signal.addEventListener("abort",stopRecovery,{once:true});
 lifecycle.current=controller;setRun(undefined);setError("");mutating.current=true;setBusy(true);
 if(!id||!tenant){setError("Run is required");setBusy(false);return()=>{controller.abort();controller.signal.removeEventListener("abort",stopRecovery)}}
 let loaded=false,loadFailed=false,hadPending=false;
 monitor.current=watchRun({load:signal=>readDeadline(signal,child=>client.request<RunStatus>(root,{signal:child})),stream:(cursor,receive,signal)=>client.eventStream(`/api/v1/tenants/${encodeURIComponent(tenant)}/events`,cursor,receive,signal)},id,controller.signal,value=>{loaded=true;setRun(value);setError(previous=>previous==="Unable to load run"?"":previous);if(!mutating.current)setBusy(false)},error=>{loadFailed=true;if(error instanceof APIError&&[401,403,404,410].includes(error.status)){controller.abort();monitor.current=undefined;mutating.current=false;setRun(undefined)}setError("Unable to load run");if(!mutating.current)setBusy(false)});
 void (async()=>{
  try{
   if(!canEdit)return;
   await readDeadline(recoveryController.signal,async recoverySignal=>{
   const user=(await api.me(recoverySignal)).id;
   recoverySignal.throwIfAborted();
   const retry=new RunRetryAttemptStore(AsyncStorage,auth.origin,user,tenant,id);
   const cancel=new RunCancelAttemptStore(AsyncStorage,auth.origin,user,tenant,id);
   const recovered=await recoverPendingRunMutations<RunStatus>({retry,cancel,signal:recoverySignal,
    send:async(action,key,body,signal)=>{hadPending=true;const value=await client.request<unknown>(`${root}/${action}`,{method:"POST",body,idempotencyKey:key,signal});signal.throwIfAborted();return readRunStatusResponse(value,id)},
    accept:value=>monitor.current?.accept(value)});
   recoverySignal.throwIfAborted();
   if(recovered&&lifecycle.current===controller)await monitor.current?.refresh();
   recoverySignal.throwIfAborted();
   },10000);
  }catch(error){
   if(lifecycle.current===controller&&!controller.signal.aborted){
    if(error instanceof APIError&&[401,403,404,410].includes(error.status)){controller.abort();monitor.current=undefined;setRun(undefined);setError("Unable to load run")}
    else setError(hadPending?"Unable to restore run action":"Unable to check saved run actions");
   }
  }finally{
   controller.signal.removeEventListener("abort",stopRecovery);
   if(lifecycle.current===controller){mutating.current=false;if(loaded||loadFailed)setBusy(false)}
  }
 })();
 return()=>{controller.abort();recoveryController.abort();monitor.current=undefined;lifecycle.current=undefined};
},[api,auth,client,root,id,tenant,reload,canEdit,workspace?.actor]);async function mutate(action:"cancel"|"retry"){
 const active=monitor.current,scope=lifecycle.current;
 if(!canEdit||!run||!active||!scope||scope.signal.aborted||mutating.current)return;
 const flight=new AbortController();mutationFlight.current=flight;
 const abort=()=>flight.abort();scope.signal.addEventListener('abort',abort,{once:true});
 const own=()=>monitor.current===active&&lifecycle.current===scope&&mutationFlight.current===flight&&!scope.signal.aborted;
 mutating.current=true;setBusy(true);setConfirming(true);
 try{
  const result=await readDeadline(flight.signal,async signal=>{
   const check=()=>{signal.throwIfAborted();if(!own())throw new Error('RUN_MUTATION_SCOPE_CHANGED');};
   const user=(await api.me(signal)).id;check();
   const retryJournal=action==='retry'?new RunRetryAttemptStore(AsyncStorage,auth.origin,user,tenant||'',id||''):undefined;
   const cancelJournal=action==='cancel'?new RunCancelAttemptStore(AsyncStorage,auth.origin,user,tenant||'',id||''):undefined;
   const retryAttempt=retryJournal?await retryJournal.begin(run.revision):undefined;
   const cancelAttempt=cancelJournal?await cancelJournal.begin():undefined;check();
   const body=retryAttempt?{expected_revision:retryAttempt.expected_revision}:undefined;
   let response:RunStatus;
   try{response=await admitRunMutation(own,signal,()=>client.request<RunStatus>(`${root}/${action}`,{method:'POST',body,idempotencyKey:retryAttempt?.key||cancelAttempt?.key,signal}));}
   catch(error){
    check();
    if(runMutationDefinitelyRejected(action,error)){
     try{if(retryAttempt)await retryJournal!.discard(retryAttempt.key);if(cancelAttempt)await cancelJournal!.discard(cancelAttempt.key);}
     catch{/* Retain any journal whose local deletion cannot be confirmed. */}
    }
    throw error;
   }
   check();response=readRunStatusResponse(response,id);if(retryAttempt)await retryJournal!.acknowledge(retryAttempt.key);check();
   if(cancelAttempt)await cancelJournal!.acknowledge(cancelAttempt.key);check();return response;
  },15000);
  if(!own())return;active.accept(result);setError('');
 }catch(error){if(own()){
  if(error instanceof APIError&&[401,403,404,410].includes(error.status)){scope.abort();monitor.current=undefined;setRun(undefined);setBusy(false);}
  setError(`Unable to ${action} run`);
 }}finally{
  scope.signal.removeEventListener('abort',abort);
  if(mutationFlight.current===flight){mutationFlight.current=null;mutating.current=false;if(lifecycle.current===scope){setBusy(false);setConfirming(false);}}
 }
}const displayedMutation=mutationFlight.current,displayedScope=lifecycle.current;const stopDisplayedMutation=()=>{if(displayedMutation&&displayedScope&&mutationFlight.current===displayedMutation&&lifecycle.current===displayedScope&&!displayedScope.signal.aborted)displayedMutation.abort();};if(!run)return <SafeAreaView style={s.root}><ReadResourceState phase={busy?"loading":"error"} retry={()=>setReload(value=>value+1)} cancel={()=>{lifecycle.current?.abort();monitor.current=undefined;setBusy(false);setError("Unable to load run");}}/></SafeAreaView>;const progress=Math.max(0,Math.min(1,run?.progress||0));return <SafeAreaView style={s.root}><View style={s.header}><Text accessibilityRole="header" style={s.title}>{__r2Value(run?.kind)||__r2T('ui_00d60e31a4e6')}</Text><Text style={s.subtitle}>{run?.stage} · {Math.round(progress*100)}%</Text>{confirming?<Button accessibilityRole="button" onPress={stopDisplayedMutation}    mode="contained" contentStyle={{minHeight:48}}>{__uiT('ui_3962912357c4')}</Button>:null}{error?<Text accessibilityRole="alert" style={s.error}>{__uiMessage(error,true)}</Text>:null}</View><ScrollView contentContainerStyle={x.body}>{runRestartNotice(run?.errors)?<Text accessibilityRole="alert" style={s.subtitle}>{runRestartNotice(run?.errors)}</Text>:null}{run&&run.kind==="export"&&terminal.has(run.status)?<ContactSheetRunOutputs key={JSON.stringify([id,run.revision])} api={api} run={id} onAccessDenied={()=>{lifecycle.current?.abort();monitor.current=undefined;setRun(undefined);setBusy(false);setError("Unable to load run outputs");}}/>:null}{run&&run.kind==="v2"?<MultiRunReviewView api={api} origin={auth.origin} tenant={tenant} run={id} onOpen={(asset,version)=>router.push({pathname:"/editor/[version]",params:{tenant,asset,version}})} onAccessDenied={()=>{lifecycle.current?.abort();monitor.current=undefined;setRun(undefined);setBusy(false);setError("Unable to read processing result.");}}/>:null}{run&&run.kind==="v2"&&!deliveryWorkflow?<DeliveryRunDiscovery api={api} run={id} onOpen={pkg=>router.push({pathname:"/deliveries/[id]",params:{tenant,project:pkg.project_id,id:pkg.id}})}/>:null}{run&&deliveryWorkflow&&deliveryProject?<DeliveryRunLink api={api} run={id} workflow={deliveryWorkflow} project={deliveryProject} onOpen={packageID=>router.push({pathname:"/deliveries/[id]",params:{tenant,project:deliveryProject,id:packageID}})}/>:null}{run&&run.kind==="v2"&&!deliveryWorkflow?<SemanticRunLink client={client} tenant={tenant} run={id} workflow={semanticWorkflow} onOpen={intent=>router.push({pathname:"/semantic/[id]",params:{tenant,id:intent}})}/>:null}<View accessibilityRole="progressbar" accessibilityValue={{min:0,max:100,now:Math.round(progress*100)}} style={x.track}><View style={[x.fill,{width:`${progress*100}%`}]}/></View>{run&&run.kind==="v2"&&run.status==="completed"&&run.counts.review===0&&!localPortraitWorkflow?<LocalPortraitRunDiscovery api={api} run={id} onOpen={saved=>router.push({pathname:"/editor/[version]",params:{tenant,asset:saved.asset_id,version:saved.asset_version_id}})} onAccessDenied={()=>{lifecycle.current?.abort();monitor.current=undefined;setRun(undefined);setBusy(false);setError("Unable to read processing result.");}}/>:null}{run&&run.kind==="v2"&&run.status==="completed"&&run.counts.review===0&&localPortraitWorkflow?<LocalPortraitRunResult api={api} workflow={localPortraitWorkflow} run={id} onOpen={saved=>router.push({pathname:"/editor/[version]",params:{tenant,asset:saved.asset_id,version:saved.asset_version_id}})} onAccessDenied={()=>{lifecycle.current?.abort();monitor.current=undefined;setRun(undefined);setBusy(false);setError("Unable to read processing result.");}}/>:null}{run&&relightWorkflow&&relightAsset&&relightVersion?<Button accessibilityRole="button" onPress={()=>router.push({pathname:"/editor/[version]",params:{tenant,asset:relightAsset,version:relightVersion}})}    mode="contained" contentStyle={{minHeight:48}}>{__uiT('ui_a81597a98e37')}</Button>:null}{run&&relightWorkflow&&run.counts.review>0?<RelightRunReviews api={api} run={id} workflow={relightWorkflow} onOpen={review=>router.push({pathname:"/reviews/[id]",params:{tenant,id:review}})}/>:null}{run&&(run.kind==="v2"||run.kind==="apply")&&!relightWorkflow?<RunReviews api={api} run={id} onAccessDenied={()=>{lifecycle.current?.abort();monitor.current=undefined;setRun(undefined);setBusy(false);setError("Unable to read processing status.");}} onOpen={review=>router.push({pathname:"/reviews/[id]",params:{tenant,id:review}})}/>:null}<View style={x.counts}><Text style={x.count}>✓ {run?.counts.succeeded||0}</Text><Text style={x.count}>! {run?.counts.failed||0}</Text><Text style={x.count}>{__uiT('ui_aff0766a5290')}{' '}{run?.counts.review||0}</Text></View>{run&&!terminal.has(run.status)?<Button accessibilityRole="button" disabled={!canEdit||busy} onPress={()=>mutate("cancel")}    mode="contained" contentStyle={{minHeight:48}}>{__uiT('ui_19766ed6ccb2')}</Button>:null}{run&&(run.status==="failed"||run.status==="completed_with_errors")?<Button accessibilityRole="button" disabled={!canEdit||busy} onPress={()=>mutate("retry")}    mode="contained" contentStyle={{minHeight:48}}>{__uiT('ui_2630f6505062')}</Button>:null}{run?.output_collection_id?<Button accessibilityRole="button" onPress={()=>router.push({pathname:"/collections/[id]",params:{id:run.output_collection_id!,tenant}})}    mode="contained" contentStyle={{minHeight:48}}>{__uiT('ui_412d6fa81fde')}</Button>:null}</ScrollView></SafeAreaView>}
const x=StyleSheet.create({body:{padding:20,gap:18},track:{height:10,backgroundColor:"#555",borderRadius:8,overflow:"hidden"},fill:{height:"100%",backgroundColor:"#d0bcff"},counts:{flexDirection:"row",gap:20},count:{color:"#ddd"}});

export default function RunScreen(){
 const params=useLocalSearchParams(),auth=useAuth(),foreground=useReviewForeground();
 const client=useMemo(()=>auth.client(),[auth]),generation=useMemo(()=>crypto.randomUUID(),[client]);
 if(!foreground.active)return null;
 return <RunScope key={JSON.stringify([generation,foreground.generation,auth.origin,params])}/>;
}
