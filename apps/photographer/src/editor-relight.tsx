import {RelightAnalysisAction} from './relight-analysis-action';
import {RelightAnalysisAttemptStore} from './relight-analysis-attempt-store';
import type {RelightAnalysisOperation} from './relight-analysis';
import {useEffect,useMemo,useRef,useState} from 'react';
import {AppState,Pressable,Text,View} from 'react-native';
import AsyncStorage from '@react-native-async-storage/async-storage';
import type {OwnerAPI} from './owner-api';
import type {V2VersionBinding} from './catalog-types';
import {RelightAttemptStore} from './relight-attempt-store';
import {RelightControls,type RelightLabels} from './relight-controls';
import {collectRelightCandidates,type RelightCandidate,type RelightCandidateKind} from './relight-candidates';
import type {RelightIntent} from './relight-intent';
import type {RegionJobTicket} from './region-preview';
import {useLocale} from './locale-context';
type Props={api:OwnerAPI;origin:string;tenant:string;project:string;asset:string;version:string;disabled?:boolean;onSubmitted:(ticket:RegionJobTicket)=>void};
type Context={user:string;store:RelightAttemptStore;base:V2VersionBinding;snapshot:string;choices:Record<RelightCandidateKind,RelightCandidate[]>};
export function EditorRelight(props:Props){
 const scope=useMemo(()=>crypto.randomUUID(),[props.api,props.origin,props.tenant,props.project,props.asset,props.version]);
 const [generation,setGeneration]=useState(0),[foreground,setForeground]=useState((AppState?.currentState??'active')==='active');
 useEffect(()=>{
  if(!AppState?.addEventListener)return;
  const listener=AppState.addEventListener('change',state=>{setForeground(state==='active');if(state==='active')setGeneration(value=>value+1);});
  return()=>listener.remove();
 },[]);
 return foreground?<Scope key={`${scope}:${generation}`} {...props} onAnother={()=>setGeneration(value=>value+1)}/>:null;
}
function Scope(props:Props&{onAnother:()=>void}){
 const {t}=useLocale();const[context,setContext]=useState<Context|null>(null),[prepared,setPrepared]=useState<RelightIntent|null>(null),[store,setStore]=useState<RelightAttemptStore|null>(null),[busy,setBusy]=useState(true),[error,setError]=useState(''),[selected,setSelected]=useState<Partial<Record<RelightCandidateKind,RelightCandidate>>>({}),[subjectKind,setSubjectKind]=useState<'person'|'manual'>('person');
 const live=useRef(true),flight=useRef<AbortController|null>(null),preparing=useRef(false);
 const labels:RelightLabels={title:t('relight_title'),approximate:t('relight_approximate'),strength:t('relight_strength'),light:t('relight_light'),x:t('relight_x'),y:t('relight_y'),radius:t('relight_radius'),ev:t('relight_ev'),add:t('relight_add'),remove:t('relight_remove'),submit:t('relight_submit'),retry:t('relight_retry'),open:t('relight_open'),loading:t('relight_loading'),unknown:t('relight_unknown'),invalid:t('relight_invalid'),unavailable:t('relight_unavailable'),another:t('relight_another')};
 useEffect(()=>{
  const controller=new AbortController();flight.current=controller;live.current=true;
  const active=()=>live.current&&!controller.signal.aborted;
  void(async()=>{try{
   const user=await props.api.me(controller.signal);if(!active())return;
   const journal=new RelightAttemptStore(AsyncStorage,props.origin,user.id,props.tenant,props.project,props.version);
   const saved=await journal.read();if(!active())return;setStore(journal);
   if(saved){setPrepared(saved.body);return;}
   const base=await props.api.v2Binding(props.asset,controller.signal);if(!active())return;
   if(base.asset_version_id!==props.version)throw new Error('SOURCE_CHANGED');
   const snapshot=await props.api.createRenderInputSnapshot(props.project,{sources:[base]},crypto.randomUUID(),controller.signal);if(!active())return;
   const kinds:RelightCandidateKind[]=['depth','person','manual_target','manual_guard'];
   const pages=await Promise.all(kinds.map(kind=>collectRelightCandidates((query,signal)=>props.api.relightCandidates(props.project,query,signal),{snapshot_id:snapshot.id,base,kind,after:null},controller.signal)));
   if(!active())return;
   const choices=Object.fromEntries(kinds.map((kind,i)=>[kind,pages[i]])) as Record<RelightCandidateKind,RelightCandidate[]>;
   setContext({user:user.id,store:journal,base,snapshot:snapshot.id,choices});
   const initial:Partial<Record<RelightCandidateKind,RelightCandidate>>={};for(const kind of kinds)if(choices[kind].length===1)initial[kind]=choices[kind][0];
   setSelected(initial);if(choices.person.length===0)setSubjectKind('manual');
  }catch{if(active())setError(t('relight_unavailable'));}finally{if(active())setBusy(false);}})();
  return()=>{live.current=false;controller.abort();flight.current?.abort();};
 },[]);
 const analysisStores=useMemo(()=>context?Object.fromEntries((['depth.predict','mask.person'] as RelightAnalysisOperation[]).map(operation=>[operation,new RelightAnalysisAttemptStore(AsyncStorage,props.origin,context.user,props.tenant,props.project,props.version,operation)])) as Record<RelightAnalysisOperation,RelightAnalysisAttemptStore>:null,[context,props.origin,props.tenant,props.project,props.version]);
 async function prepare(){
  const subject=selected[subjectKind==='person'?'person':'manual_target'];
  if(!context||!selected.depth||!subject||!selected.manual_guard||preparing.current||props.disabled)return;
  preparing.current=true;setBusy(true);setError('');const controller=new AbortController();flight.current=controller;
  try{
   const body=await props.api.prepareRelight(props.project,{schema_version:1,snapshot_id:context.snapshot,base:context.base,depth:selected.depth.ref,subject:subject.ref,protected:selected.manual_guard.ref,subject_kind:subjectKind,ordinal:0},controller.signal);
   if(live.current&&!controller.signal.aborted)setPrepared(body);
  }catch{if(live.current&&!controller.signal.aborted)setError(t('relight_unavailable'));}
  finally{preparing.current=false;if(live.current)setBusy(false);}
 }
 async function verifyTicket(ticket:RegionJobTicket,signal:AbortSignal){
  const status=await props.api.get<{run_id:string;workflow_id:string}>(`/api/v1/tenants/{t}/v2-workflows/${encodeURIComponent(ticket.workflow_id)}`,signal);
  if(status.run_id!==ticket.run_id||status.workflow_id!==ticket.workflow_id)throw new Error('RELIGHT_RECOVERY_SCOPE_CHANGED');
 }
 if(prepared&&store)return <RelightControls api={props.api} project={props.project} prepared={prepared} store={store} labels={labels} disabled={props.disabled} verifyTicket={verifyTicket} onSubmitted={props.onSubmitted} onAnother={props.onAnother}/>;
 function options(kind:RelightCandidateKind,label:string){const choices=context?.choices[kind]??[];return <View><Text>{label}</Text>{choices.length===0?<Text>{t('relight_missing_inputs')}</Text>:choices.map((item,index)=><Pressable key={item.ref.artifact_id} accessibilityRole="radio" accessibilityState={{checked:selected[kind]?.ref.artifact_id===item.ref.artifact_id}} disabled={busy||props.disabled} onPress={()=>setSelected(old=>({...old,[kind]:item}))}><Text>{label} {index+1}</Text></Pressable>)}</View>;}
 return <View><Text>{labels.title}</Text><Text>{labels.approximate}</Text>{busy?<Text>{labels.loading}</Text>:null}{error?<Text accessibilityRole="alert">{error}</Text>:null}{context?<>
 {analysisStores?(['depth.predict','mask.person'] as RelightAnalysisOperation[]).map(operation=><RelightAnalysisAction key={operation} api={props.api} project={props.project} intent={{schema_version:1,snapshot_id:context.snapshot,base:context.base,operation}} store={analysisStores[operation]} available={context.choices[operation==='depth.predict'?'depth':'person'].length>0} disabled={busy||props.disabled} labels={{create:t(operation==='depth.predict'?'relight_generate_depth':'relight_generate_person'),retry:labels.retry,open:labels.open,loading:labels.loading,unknown:labels.unknown,unavailable:labels.unavailable}} progressLabels={{queued:t('relight_analysis_queued'),running:t('relight_analysis_running'),waiting_review:t('relight_analysis_waiting_review'),cancelling:t('relight_analysis_cancelling'),cancelled:t('relight_analysis_cancelled'),completed:t('relight_analysis_completed'),failed:t('relight_analysis_failed'),unavailable:t('relight_unavailable'),refresh:t('relight_analysis_refresh')}} onRefresh={props.onAnother} verifyTicket={verifyTicket} onSubmitted={props.onSubmitted}/>):null}
 {options('depth',t('relight_depth'))}
 <Pressable disabled={busy||props.disabled} onPress={()=>setSubjectKind('person')}><Text>{t('relight_person')}</Text></Pressable><Pressable disabled={busy||props.disabled} onPress={()=>setSubjectKind('manual')}><Text>{t('relight_manual')}</Text></Pressable>
 {options(subjectKind==='person'?'person':'manual_target',t('relight_subject'))}{options('manual_guard',t('relight_protection'))}
 <Pressable disabled={busy||props.disabled||!selected.depth||!selected[subjectKind==='person'?'person':'manual_target']||!selected.manual_guard} onPress={()=>void prepare()}><Text>{t('relight_prepare')}</Text></Pressable>
 </>:null}</View>;
}
