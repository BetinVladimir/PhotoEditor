import {Button,TouchableRipple,ActivityIndicator,Text} from 'react-native-paper';
import {useWorkspaceAccess} from '../../../src/workspace-access-context';
import {persistSessionLifecycleAttempt} from '../../../src/session-lifecycle-attempt';
import {SessionPolicyDraftStore} from '../../../src/session-policy-draft';
import {requireSessionAnalysisAvailable} from '../../../src/session-operation-availability';
import {SessionAnalysisAttemptStore,type SessionAnalysisAttempt} from '../../../src/session-analysis-attempt-store';
import AsyncStorage from '@react-native-async-storage/async-storage';
import {revokeSessionPolicyDrafts} from '../../../src/session-policy-draft';
import {SessionLifecycle} from '../../../src/session-lifecycle';
import {PersistentSessionPolicyEditor} from '../../../src/persistent-session-policy-editor';
import type {ShootSessionUpdate} from '../../../src/capture-types';
import {initialSceneAnalysisRequest} from '../../../src/scene-analysis-request';
import {useEffect,useMemo,useRef,useState} from 'react';
import {router,useLocalSearchParams} from '../../../src/localized-router';
import {APIError} from '@photoeditor/api-client';
import {FlatList,SafeAreaView,View} from 'react-native';
import {useAuth} from '../../../src/auth-context';
import {useLocale} from '../../../src/locale-context';
import {LocalePicker} from '../../../src/locale-picker';
import {OwnerAPI} from '../../../src/owner-api';
import type {ShootSession} from '../../../src/capture-types';
import type {SessionAnalysisSummary} from '../../../src/session-analysis-list';
import {catalogStyle as s} from '../../../src/catalog-style';
import {useReviewForeground} from '../../../src/use-review-foreground';
export default function SessionPage(){
 const foreground=useReviewForeground();
 const {id,tenant}=useLocalSearchParams<{id:string;tenant:string}>();const auth=useAuth();
 const client=useMemo(()=>auth.client(),[auth]);
 const api=useMemo(()=>new OwnerAPI(client,tenant||''),[client,tenant]);
 const generation=useMemo(()=>crypto.randomUUID(),[client]);
 if(!foreground.active)return null;
 return <SessionScope key={JSON.stringify([generation,foreground.generation,auth.origin,id,tenant])} id={id} api={api} origin={auth.origin}/>;
}
function SessionScope({id,api,origin}:{id:string;api:OwnerAPI;origin:string}){
 const roleCanEdit=useWorkspaceAccess(api.tenant)?.canEdit===true;
 const {t}=useLocale();
 function revokeDraft(){void revokeSessionPolicyDrafts(AsyncStorage,origin,api.tenant,id).catch(()=>setError('scene_access_denied'));}
 const lifetime=useRef<AbortController|null>(null),loading=useRef(false),cursors=useRef(new Set<string>());
 const [policyDirty,setPolicyDirty]=useState(false),[statePending,setStatePending]=useState(false);
 const attempt=useRef<SessionAnalysisAttempt|null>(null),journal=useRef<SessionAnalysisAttemptStore|null>(null);
 const [reload,setReload]=useState(0),[busy,setBusy]=useState(true),[error,setError]=useState<'session_load_failed'|'scene_reference_failed'|'scene_reference_conflict'|'scene_reference_unavailable'|'scene_access_denied'|null>(null);
 const [loaded,setLoaded]=useState<{api:OwnerAPI;session:ShootSession;canEdit:boolean;items:SessionAnalysisSummary[];cursor:string|null}>();
 useEffect(()=>{
  const controller=new AbortController();lifetime.current=controller;loading.current=false;attempt.current=null;journal.current=null;cursors.current.clear();setLoaded(undefined);setBusy(true);setError(null);
  async function load(){try{if(!id||!api.tenant)throw new Error('SESSION_SCOPE');const user=await api.me(controller.signal);if(controller.signal.aborted)return;const store=new SessionAnalysisAttemptStore(AsyncStorage,origin,user.id,api.tenant,id),saved=await store.read();if(controller.signal.aborted)return;journal.current=store;if(saved){attempt.current=saved;setError('scene_reference_failed');if(saved.ticket){await acknowledge(store,saved,controller);return;}}const [session,page]=await Promise.all([api.shootSession(id,controller.signal),api.sessionAnalyses(id,undefined,controller.signal)]);if(controller.signal.aborted)return;const access=await api.projectAccess(session.project_id,controller.signal);if(!controller.signal.aborted)setLoaded({api,session,canEdit:roleCanEdit&&access.can_edit,items:page.items,cursor:page.next_cursor??null});}catch(error){if(!controller.signal.aborted){if(error instanceof APIError&&[401,403,404,410].includes(error.status)){revokeDraft();attempt.current=null;journal.current=null;}setError('session_load_failed');}}finally{if(!controller.signal.aborted)setBusy(false);}}
  void load();return()=>{controller.abort();if(lifetime.current===controller)lifetime.current=null;};
 },[id,api,reload,origin,roleCanEdit]);
 function cancelWaiting(){lifetime.current?.abort();loading.current=false;setLoaded(undefined);setBusy(false);setPolicyDirty(false);setStatePending(false);setError('session_load_failed');}
 useEffect(()=>{if(!busy)return;const timer=setTimeout(cancelWaiting,loaded?15000:8000);return()=>clearTimeout(timer);},[busy,loaded]);
 async function more(){
  const controller=lifetime.current;if(!loaded||loaded.api!==api||!loaded.cursor||!controller||controller.signal.aborted||loading.current)return;
  loading.current=true;setBusy(true);setError(null);const cursor=loaded.cursor;
  try{
   const page=await api.sessionAnalyses(id,cursor,controller.signal);if(controller.signal.aborted)return;
   if(cursors.current.has(cursor)||page.next_cursor===cursor||page.items.some(item=>loaded.items.some(previous=>previous.id===item.id))||loaded.items.length+page.items.length>5000)throw new Error('SESSION_PAGINATION_INVALID');
   cursors.current.add(cursor);setLoaded({...loaded,items:[...loaded.items,...page.items],cursor:page.next_cursor??null});
  }catch(error){if(!controller.signal.aborted){if(error instanceof APIError&&[401,403,404,410].includes(error.status)){revokeDraft();setLoaded(undefined);}setError('session_load_failed');}}
  finally{if(!controller.signal.aborted){loading.current=false;setBusy(false);}}
 }
 async function readCurrentPolicy(){
  const controller=lifetime.current;
  if(!controller||controller.signal.aborted)throw new Error('SESSION_CANCELLED');
  try{const current=await api.shootSession(id,controller.signal);if(controller.signal.aborted||lifetime.current!==controller)throw new Error('SESSION_CANCELLED');return current;}
  catch(error){if(!controller.signal.aborted&&error instanceof APIError&&[401,403,404,410].includes(error.status)){revokeDraft();setLoaded(undefined);setError('scene_access_denied');}throw error;}
 }
 async function savePolicy(request:ShootSessionUpdate,key:string){
  const controller=lifetime.current;
  if(!loaded||!loaded.canEdit||loaded.api!==api||!controller||controller.signal.aborted||loading.current||attempt.current)throw new Error('SESSION_BUSY');
  loading.current=true;setBusy(true);setError(null);
  let lifecycleStore:SessionPolicyDraftStore|undefined;let sent=false;
  try{
   if(request.state!==loaded.session.state){
    const user=await api.me(controller.signal);if(controller.signal.aborted)throw new Error('SESSION_CANCELLED');
    lifecycleStore=new SessionPolicyDraftStore(AsyncStorage,origin,user.id,api.tenant,id);
    await persistSessionLifecycleAttempt(lifecycleStore,loaded.session,request,key);
    if(controller.signal.aborted)throw new Error('SESSION_CANCELLED');
   }
   sent=true;const session=await api.updateShootSession(id,request,key,controller.signal);
   if(controller.signal.aborted||lifetime.current!==controller)throw new Error('SESSION_CANCELLED');
   if(lifecycleStore)await lifecycleStore.clearAttempt(key);
   if(controller.signal.aborted)throw new Error('SESSION_CANCELLED');
   setLoaded({...loaded,session:BigInt(session.revision)<BigInt(loaded.session.revision)?loaded.session:session});setPolicyDirty(false);
  }catch(error){if(!controller.signal.aborted){if(sent&&lifecycleStore&&error instanceof APIError&&[409,410,412,422].includes(error.status))await lifecycleStore.clearAttempt(key);if(error instanceof APIError&&[401,403,404,410].includes(error.status)){revokeDraft();setLoaded(undefined);}setError(error instanceof APIError&&[409,412].includes(error.status)?'scene_reference_conflict':'session_load_failed');}throw error;}
  finally{if(!controller.signal.aborted){loading.current=false;setBusy(false);}}
 }
 async function acknowledge(store:SessionAnalysisAttemptStore,record:SessionAnalysisAttempt,controller:AbortController){
  if(!record.ticket)throw new Error('SESSION_ANALYSIS_RECEIPT_MISSING');
  const run=await api.run(record.ticket.run_id,controller.signal);
  if(controller.signal.aborted||lifetime.current!==controller)return;
  if(run.id!==record.ticket.run_id)throw new Error('SESSION_ANALYSIS_RECEIPT_INVALID');
  router.push({pathname:'/runs/[id]',params:{id:record.ticket.run_id,tenant:api.tenant}});
  await store.clearAccepted(record.key,record.ticket.run_id);
  if(!controller.signal.aborted&&lifetime.current===controller)attempt.current=null;
 }
 async function analyze(){
  const controller=lifetime.current,store=journal.current;
  if((!loaded||loaded.api!==api)&&!attempt.current||!store||!controller||controller.signal.aborted||loading.current)return;
  loading.current=true;setBusy(true);setError(null);let sent=false;
  try{
   if(!attempt.current){
    if(!loaded||!loaded.canEdit||policyDirty||loaded.session.state!=='open')throw new Error('SESSION_CLOSED');
    await requireSessionAnalysisAvailable(api,controller.signal);if(controller.signal.aborted)return;
    const collection=await api.collection(loaded.session.collection_id,controller.signal);if(controller.signal.aborted)return;
    const snapshot=await api.selectionSnapshot(collection,undefined,controller.signal);if(controller.signal.aborted)return;
    const request=initialSceneAnalysisRequest(loaded.session,snapshot.id);
    await api.sessionAnalysisCapabilities(id,request,controller.signal);if(controller.signal.aborted||lifetime.current!==controller)return;
    attempt.current={schema_version:1,session_id:id,request:request,key:crypto.randomUUID(),ticket:null};
   }
   let current=attempt.current;
   const saved=await store.read();if(controller.signal.aborted)return;
   if(saved){
    if(saved.key!==current.key||JSON.stringify(saved.request)!==JSON.stringify(current.request))throw new Error('SESSION_ANALYSIS_JOURNAL_CONFLICT');
    current=saved;attempt.current=saved;
   }
   if(current.ticket){await acknowledge(store,current,controller);return;}
   await store.write(current);if(controller.signal.aborted)return;
   sent=true;const ticket=await api.analyzeSession(id,current.request,current.key,controller.signal);
   if(controller.signal.aborted||lifetime.current!==controller)return;
   const accepted={...current,ticket:{run_id:ticket.run_id}};
   await store.write(accepted);if(controller.signal.aborted)return;
   attempt.current=accepted;sent=false;await acknowledge(store,accepted,controller);
  }catch(error){if(!controller.signal.aborted){
   if(error instanceof APIError&&[401,403,404,410].includes(error.status)){revokeDraft();attempt.current=null;setLoaded(undefined);setError('scene_access_denied');}
   else if(sent&&error instanceof APIError&&[409,410,412,422].includes(error.status)&&attempt.current){
    try{await store.discardRejected(attempt.current.key);if(controller.signal.aborted)return;attempt.current=null;setError('scene_reference_conflict');}catch{if(!controller.signal.aborted)setError('scene_reference_failed');}
   }else setError(error instanceof APIError&&error.status===503?'scene_reference_unavailable':'scene_reference_failed');
  }}finally{if(!controller.signal.aborted){loading.current=false;setBusy(false);}}
 }

 const data=loaded?.api===api?loaded:undefined;
 return <SafeAreaView style={s.root}><View style={s.header}><LocalePicker/><Text accessibilityRole="header" style={s.title}>{data?.session.name||t('session_title')}</Text><Button accessibilityRole="button" disabled={busy||!lifetime.current?.signal.aborted&&(policyDirty||statePending||!!attempt.current)} onPress={()=>{if(lifetime.current?.signal.aborted||!loading.current&&!attempt.current)setReload(value=>value+1);}}    mode="contained" contentStyle={{minHeight:48}}>{t('scene_reload')}</Button>{error?<Text accessibilityRole="alert" style={s.error}>{t(error)}</Text>:null}</View>
  {data?.canEdit?<SessionLifecycle key={"state:"+data.session.revision} session={data.session} disabled={busy||policyDirty||!!attempt.current} onSave={savePolicy} onPending={setStatePending}/>:null}
  {data?.canEdit?<PersistentSessionPolicyEditor api={api} origin={origin} key={data.session.revision} session={data.session} disabled={busy||statePending||!!attempt.current} onReadCurrent={readCurrentPolicy} onUseCurrent={current=>{if(lifetime.current&&!lifetime.current.signal.aborted&&loaded?.api===api&&current.id===id){setLoaded({...loaded,session:current});setPolicyDirty(false);}}} onSave={savePolicy} onDirty={setPolicyDirty}/>:null}
  {!lifetime.current?.signal.aborted&&(data?.canEdit&&data.session.state==='open'||attempt.current&&(data?.canEdit!==false||attempt.current.ticket))?<Button accessibilityRole="button" disabled={busy||!attempt.current&&(policyDirty||statePending)} onPress={()=>void analyze()}    mode="contained" contentStyle={{minHeight:48}}>{t(attempt.current?'scene_retry_analysis':'session_analyze')}</Button>:null}
  {busy?<View><ActivityIndicator/><Button accessibilityRole="button" onPress={cancelWaiting} mode="outlined">{t('read_cancel')}</Button></View>:null}<FlatList data={data?.items??[]} keyExtractor={item=>item.id} initialNumToRender={10} windowSize={5} ListEmptyComponent={!busy?<Text style={s.empty}>{t('session_no_analyses')}</Text>:null}
   renderItem={({item,index})=><TouchableRipple accessibilityRole="button" onPress={()=>router.push({pathname:'/sessions/[id]/scenes',params:{id,tenant:api.tenant,analysis:item.id}})} style={s.button}><View style={{alignItems:'center',justifyContent:'center'}}><Text style={s.buttonText}>{t('session_analysis')} {index+1}</Text><Text style={s.meta}>{item.createdAt}</Text></View></TouchableRipple>}
   ListFooterComponent={data?.cursor?<Button accessibilityRole="button" disabled={busy||!!attempt.current} onPress={()=>void more()}    mode="contained" contentStyle={{minHeight:48}}>{t('session_more')}</Button>:null}/>
 </SafeAreaView>;
}
