import {Button,ActivityIndicator,Text} from 'react-native-paper';
import {useWorkspaceAccess} from '../../../src/workspace-access-context';
import {requireSessionAnalysisAvailable} from '../../../src/session-operation-availability';
import {APIError} from '@photoeditor/api-client';
import AsyncStorage from '@react-native-async-storage/async-storage';
import {sceneReferenceRequest,type SceneAnalysisRequest} from '../../../src/scene-analysis-request';
import {useEffect,useMemo,useRef,useState} from 'react';
import {router,useLocalSearchParams} from '../../../src/localized-router';
import {SafeAreaView,View} from 'react-native';
import {useAuth} from '../../../src/auth-context';
import {useLocale} from '../../../src/locale-context';
import {LocalePicker} from '../../../src/locale-picker';
import {OwnerAPI} from '../../../src/owner-api';
import type {SceneBoard} from '../../../src/scene-board';
import {SceneApplyPanel} from '../../../src/scene-apply-panel';
import {SceneApplyAttemptStore,type SceneApplyAttempt} from '../../../src/scene-apply-attempt-store';
import {SceneReferenceAttemptStore} from '../../../src/scene-reference-attempt-store';
import {SceneBoardView} from '../../../src/scene-board-view';
import {catalogStyle as s} from '../../../src/catalog-style';
import {useReviewForeground} from '../../../src/use-review-foreground';
export default function SessionScenes(){
 const {id,tenant,analysis}=useLocalSearchParams<{id:string;tenant:string;analysis:string}>();
 const foreground=useReviewForeground();
 const auth=useAuth(),client=useMemo(()=>auth.client(),[auth]);
 const api=useMemo(()=>new OwnerAPI(client,tenant||''),[client,tenant]);
 const generation=useMemo(()=>crypto.randomUUID(),[client]);
 if(!foreground.active)return null;
 return <SceneScope key={JSON.stringify([generation,foreground.generation,auth.origin,id,tenant,analysis])} origin={auth.origin} id={id} analysis={analysis} api={api}/>;
}
type SceneError='scene_load_failed'|'scene_reference_failed'|'scene_reference_conflict'|'scene_reference_unavailable'|'scene_access_denied'|'scene_apply_failed'|'scene_apply_conflict';
function SceneScope({id,analysis,api,origin}:{id:string;analysis:string;api:OwnerAPI;origin:string}){
 const lifetime=useRef<AbortController|null>(null),lock=useRef(false);
 const attempt=useRef<{request:SceneAnalysisRequest;key:string}|null>(null),journal=useRef<SceneReferenceAttemptStore|null>(null),applyJournal=useRef<SceneApplyAttemptStore|null>(null);
 const roleCanEdit=useWorkspaceAccess(api.tenant)?.canEdit===true;
 const [canEdit,setCanEdit]=useState<boolean|null>(null);
 const [submitting,setSubmitting]=useState(false),[applying,setApplying]=useState(false);
 const [applyPending,setApplyPending]=useState<SceneApplyAttempt|null>(null);
 const {t}=useLocale();const [reload,setReload]=useState(0);
 const [loaded,setLoaded]=useState<{api:OwnerAPI;reload:number;board:SceneBoard}>();const [busy,setBusy]=useState(true),[error,setError]=useState<SceneError|null>(null);
 async function openRun(runId:string,controller:AbortController){
  const run=await api.run(runId,controller.signal);
  if(controller.signal.aborted||lifetime.current!==controller)return;
  if(run.id!==runId)throw new Error('SCENE_RECEIPT_INVALID');
  router.push({pathname:'/runs/[id]',params:{id:runId,tenant:api.tenant}});
 }
 useEffect(()=>{
  const controller=new AbortController();lifetime.current=controller;attempt.current=null;journal.current=null;applyJournal.current=null;setApplyPending(null);setCanEdit(null);lock.current=false;setSubmitting(false);setApplying(false);setLoaded(undefined);setError(null);setBusy(true);
  async function load(){try{if(!id||!analysis||!api.tenant)throw new Error('SCENE_SCOPE_INVALID');const user=await api.me(controller.signal);if(controller.signal.aborted||lifetime.current!==controller)return;const store=new SceneReferenceAttemptStore(AsyncStorage,origin,user.id,api.tenant,id,analysis);const saved=await store.read();if(controller.signal.aborted||lifetime.current!==controller)return;journal.current=store;if(saved){attempt.current={request:saved.request,key:saved.key};if(saved.ticket){await openRun(saved.ticket.run_id,controller);return;}setError('scene_reference_failed');}const apply=new SceneApplyAttemptStore(AsyncStorage,origin,user.id,api.tenant,id,analysis),applied=await apply.read();if(controller.signal.aborted||lifetime.current!==controller)return;applyJournal.current=apply;if(applied?.ticket){setApplyPending(applied);await openRun(applied.ticket.run_id,controller);return;}if(applied){setApplyPending(applied);setError('scene_apply_failed');}const session=await api.shootSession(id,controller.signal);if(controller.signal.aborted||lifetime.current!==controller)return;const access=await api.projectAccess(session.project_id,controller.signal);if(controller.signal.aborted||lifetime.current!==controller)return;setCanEdit(roleCanEdit&&access.can_edit);const board=await api.sceneBoard(analysis,id,controller.signal);if(!controller.signal.aborted&&lifetime.current===controller)setLoaded({api,reload,board});}catch(e){if(!controller.signal.aborted&&lifetime.current===controller){if(e instanceof APIError&&[401,403,404,410].includes(e.status)){setLoaded(undefined);attempt.current=null;journal.current=null;applyJournal.current=null;setApplyPending(null);setError('scene_access_denied');}else setError('scene_load_failed');}}finally{if(!controller.signal.aborted&&lifetime.current===controller)setBusy(false);}}
  void load();return()=>{controller.abort();if(lifetime.current===controller)lifetime.current=null;};
 },[id,analysis,api,reload,origin,roleCanEdit]);
 function cancelWaiting(){lifetime.current?.abort();lock.current=false;setLoaded(undefined);setCanEdit(null);setBusy(false);setSubmitting(false);setApplying(false);setError('scene_load_failed');}
 useEffect(()=>{if(!busy&&!submitting&&!applying)return;const timer=setTimeout(cancelWaiting,loaded?15000:8000);return()=>clearTimeout(timer);},[busy,submitting,applying,loaded]);
 async function submitReference(groupId?:string,versionId?:string){
  const controller=lifetime.current;
  if(lock.current||canEdit===false||applyPending||!controller||controller.signal.aborted||(!loaded&&!attempt.current)||!journal.current)return;
  lock.current=true;setSubmitting(true);setError(null);let sent=false;
  try{
   if(!attempt.current){
    if(!loaded||canEdit!==true||!groupId||!versionId)throw new Error('SCENE_REFERENCE_MISSING');
    await requireSessionAnalysisAvailable(api,controller.signal);if(controller.signal.aborted)return;
    const session=await api.shootSession(id,controller.signal);
    if(controller.signal.aborted)return;
    const collection=await api.collection(session.collection_id,controller.signal);
    if(controller.signal.aborted)return;
    const snapshot=await api.selectionSnapshot(collection,undefined,controller.signal);
    if(controller.signal.aborted)return;
    const request=sceneReferenceRequest(loaded.board,analysis,session,snapshot.id,groupId,versionId);
    await api.sessionAnalysisCapabilities(id,request,controller.signal);if(controller.signal.aborted||lifetime.current!==controller)return;
    attempt.current={request,key:crypto.randomUUID()};
   }
   const current=attempt.current;const saved=await journal.current.read();if(controller.signal.aborted||lifetime.current!==controller)return;if(saved&&(saved.key!==current.key||JSON.stringify(saved.request)!==JSON.stringify(current.request)))throw new Error('SCENE_REFERENCE_JOURNAL_CONFLICT');if(saved?.ticket){await openRun(saved.ticket.run_id,controller);return;}await journal.current.write({schema_version:1,session_id:id,analysis_id:analysis,key:current.key,request:current.request,ticket:null});if(controller.signal.aborted||lifetime.current!==controller)return;
   sent=true;const ticket=await api.analyzeSession(id,current.request,current.key,controller.signal);
   if(controller.signal.aborted||lifetime.current!==controller)return;
   await journal.current.write({schema_version:1,session_id:id,analysis_id:analysis,key:current.key,request:current.request,ticket:{run_id:ticket.run_id}});if(controller.signal.aborted||lifetime.current!==controller)return;
   sent=false;await openRun(ticket.run_id,controller);
  }catch(error){
   if(!controller.signal.aborted&&lifetime.current===controller){
    if(sent&&error instanceof APIError&&[409,410,412,422].includes(error.status)&&attempt.current&&journal.current){try{await journal.current.discardRejected(attempt.current.key);}catch{setError('scene_reference_failed');return;}}
    if(error instanceof APIError&&[401,403,404,410].includes(error.status)){setLoaded(undefined);attempt.current=null;setError('scene_access_denied');}
    else if(sent&&error instanceof APIError&&[409,410,412,422].includes(error.status)){attempt.current=null;setError('scene_reference_conflict');}
    else setError(error instanceof APIError&&error.status===503?'scene_reference_unavailable':'scene_reference_failed');
   }
  }
  finally{if(lifetime.current===controller&&!controller.signal.aborted){lock.current=false;setSubmitting(false);}}
 }
 async function retryApply(){const controller=lifetime.current,requested=applyPending,store=applyJournal.current;if(lock.current||canEdit===false&&!requested?.ticket||!controller||controller.signal.aborted||!requested||!store)return;lock.current=true;setSubmitting(true);setError(null);let sent=false;
  try{const current=await store.read();if(controller.signal.aborted||lifetime.current!==controller)return;if(!current||current.key!==requested.key||JSON.stringify(current.body)!==JSON.stringify(requested.body)){setApplyPending(null);setError('scene_apply_conflict');return;}if(current.ticket){await openRun(current.ticket.run_id,controller);return;}
   sent=true;const ticket=await api.applySceneAnalysis(analysis,current.body,current.key,controller.signal);if(controller.signal.aborted||lifetime.current!==controller)return;
   await store.write({...current,ticket:{run_id:ticket.run_id}});if(controller.signal.aborted||lifetime.current!==controller)return;setApplyPending({...current,ticket:{run_id:ticket.run_id}});sent=false;await openRun(ticket.run_id,controller);
  }catch(e){if(controller.signal.aborted||lifetime.current!==controller)return;if(sent&&e instanceof APIError&&[409,410,412,422].includes(e.status)){try{await store.discardRejected(requested.key);setApplyPending(null);setError('scene_apply_conflict');}catch{setError('scene_apply_failed');}}else if(e instanceof APIError&&[401,403,404,410].includes(e.status)){setLoaded(undefined);setApplyPending(null);setError('scene_access_denied');}else setError('scene_apply_failed');}
  finally{if(lifetime.current===controller&&!controller.signal.aborted){lock.current=false;setSubmitting(false);}}
 }
 const board=loaded?.api===api&&loaded.reload===reload?loaded.board:undefined;
 return <SafeAreaView style={s.root}><View style={s.header}><LocalePicker/><Button accessibilityRole="button" disabled={busy||submitting||applying||!lifetime.current?.signal.aborted&&(!!attempt.current||!!applyPending)} onPress={()=>setReload(value=>value+1)}    mode="contained" contentStyle={{minHeight:48}}>{t('scene_reload')}</Button>{!lifetime.current?.signal.aborted&&error&&attempt.current&&!applyPending&&canEdit!==false?<Button accessibilityRole="button" disabled={submitting} onPress={()=>void submitReference()}    mode="contained" contentStyle={{minHeight:48}}>{t('scene_retry_analysis')}</Button>:null}{!lifetime.current?.signal.aborted&&applyPending&&(canEdit!==false||applyPending.ticket)?<Button accessibilityRole="button" disabled={busy||submitting} onPress={()=>void retryApply()}    mode="contained" contentStyle={{minHeight:48}}>{t('scene_apply_retry')}</Button>:null}{error?<Text accessibilityRole="alert" style={s.error}>{t(error)}</Text>:null}</View>{busy||submitting||applying?<View><ActivityIndicator/><Button accessibilityRole="button" onPress={cancelWaiting} mode="outlined">{t('read_cancel')}</Button></View>:null}{board?<>{!applyPending&&canEdit===true?<SceneApplyPanel origin={origin} api={api} sessionId={id} analysisId={analysis} board={board} disabled={submitting||!!attempt.current} onBusy={setApplying} onAccessDenied={()=>{setLoaded(undefined);setError('scene_access_denied');}}/>:null}<SceneBoardView api={api} onAccessDenied={()=>{setLoaded(undefined);setError('scene_access_denied');}} board={board} busy={submitting||applying||!!attempt.current||!!applyPending} onReference={canEdit===true?(groupId,versionId)=>void submitReference(groupId,versionId):undefined}/></>:null}</SafeAreaView>;
}
