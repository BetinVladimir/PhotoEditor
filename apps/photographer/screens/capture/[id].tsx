import {Button,ActivityIndicator,Text} from 'react-native-paper';
import {SessionAnalysisNavigation} from '../../src/navigation-entry-links';
import {readDeadline} from '../../src/read-deadline';
import {useWorkspaceAccess} from '../../src/workspace-access-context';
import AsyncStorage from '@react-native-async-storage/async-storage';
import {CaptureDraftStore,clearCaptureSessionDrafts} from '../../src/capture-draft-store';
import {APIError} from '@photoeditor/api-client';
import {commands} from '../../src/commands';
import {useCommands} from '../../src/use-commands';
import {CoalescedRefresh} from "../../src/coalesced-refresh";
import {LocalePicker} from "../../src/locale-picker";
import {useLocale} from "../../src/locale-context";
import {readCaptureAlertDelta} from "../../src/capture-alert-refresh";
import {mergeCaptureAlerts} from "../../src/capture-alert-merge";
import {loadCaptureShots} from "../../src/capture-shot-load";
import type {Page} from "../../src/catalog-types";
import {CaptureShotEditor} from "../../src/capture-shot-editor";
import {useEffect,useMemo,useRef,useState} from 'react';
import {useLocalSearchParams} from '../../src/localized-router';
import {AppState,SafeAreaView,ScrollView,View} from 'react-native';
import {useAuth} from '../../src/auth-context';
import {OwnerAPI} from '../../src/owner-api';
import type {ShootSession,CaptureAlert,CaptureShotListView,CaptureShotRequirement,CapturePerson} from '../../src/capture-types';
import {catalogStyle as s} from '../../src/catalog-style';

export default function CaptureSession(){
 const {id,tenant}=useLocalSearchParams<{id:string;tenant:string}>();
 const auth=useAuth();
 const api=useMemo(()=>new OwnerAPI(auth.client(),tenant||''),[auth,tenant]);
 const roleCanEdit=useWorkspaceAccess(tenant||'')?.canEdit===true;
 const scope=useMemo(()=>crypto.randomUUID(),[api,id,tenant]);
 return <CaptureSessionScope key={scope} api={api} id={id} tenant={tenant} auth={auth} roleCanEdit={roleCanEdit}/>;
}
function CaptureSessionScope({api,id,tenant,auth,roleCanEdit}:{api:OwnerAPI;id:string;tenant:string;auth:ReturnType<typeof useAuth>;roleCanEdit:boolean}){
 const {t}=useLocale();
 const alive=useRef(true),foreground=useRef(AppState.currentState==='active');
 useEffect(()=>{alive.current=true;const listener=AppState.addEventListener('change',state=>{foreground.current=state==='active';if(!foreground.current){actionFlight.current?.abort();actionFlight.current=null;manualRead.current?.abort();manualRead.current=null;setBusy(false);}});return()=>{alive.current=false;listener.remove();};},[]);
 const [projectCanEdit,setProjectCanEdit]=useState(false);
 const canEdit=roleCanEdit&&projectCanEdit;
 const generation=useRef(0);
 const draftStore=useRef<CaptureDraftStore|null>(null);
 const actionFlight=useRef<AbortController|null>(null);
 const manualRead=useRef<AbortController|null>(null),initialRead=useRef<AbortController|null>(null);
 const [initialLoading,setInitialLoading]=useState(false);
 const [refresh,setRefresh]=useState(0);
 const [replaceBlocked,setReplaceBlocked]=useState(false);
 const [session,setSession]=useState<ShootSession>();
 const sessionAttempt=useRef<{body:string;key:string}|null>(null);
 const [shots,setShots]=useState<CaptureShotListView>();
 const [shotConflict,setShotConflict]=useState<CaptureShotListView>();
 const [people,setPeople]=useState<CapturePerson[]>([]);
 const [draft,setDraft]=useState<CaptureShotRequirement[]>([]);
 const saveAttempt=useRef<{body:string;key:string}|null>(null);
 const confirmationKeys=useRef(new Map<string,string>());
 const acknowledgementKeys=useRef(new Map<string,{revision:string;key:string}>());
 const [alerts,setAlerts]=useState<CaptureAlert[]>([]);
 const alertSnapshot=useRef(alerts);alertSnapshot.current=alerts;
 const [cursor,setCursor]=useState<string|null>(null);
 const [busy,setBusy]=useState(false),[error,setError]=useState('');
 const live=useRef({canEdit,busy,draft,shots,session,shotConflict,replaceBlocked});live.current={canEdit,busy,draft,shots,session,shotConflict,replaceBlocked};
 const active=()=>alive.current&&foreground.current;
 const editable=()=>active()&&live.current.canEdit&&!live.current.busy&&!actionFlight.current&&!manualRead.current;
 const changeDraft=(next:CaptureShotRequirement[])=>{if(editable()&&!saveAttempt.current&&live.current.draft===draft&&!live.current.replaceBlocked)setDraft(next);};
 function invalidateAccess(error:unknown,scope=generation.current){
  if(scope!==generation.current||!(error instanceof APIError)||![401,403,404,410].includes(error.status))return false;
  generation.current++;actionFlight.current?.abort();manualRead.current?.abort();initialRead.current?.abort();draftStore.current=null;
  setShots(undefined);setShotConflict(undefined);setDraft([]);setPeople([]);setAlerts([]);setSession(undefined);setCursor(null);setBusy(false);setError('capture_access_denied');
  const clearedScope=generation.current;
  void clearCaptureSessionDrafts(AsyncStorage,auth.origin,tenant,id).catch(()=>{if(clearedScope===generation.current)setError('capture_draft_storage_error')});
  return true;
 }
 useEffect(()=>{
  actionFlight.current?.abort();
  const controller=new AbortController();initialRead.current=controller;generation.current++;draftStore.current=null;setInitialLoading(true);
  setProjectCanEdit(false);setSession(undefined);sessionAttempt.current=null;setReplaceBlocked(false);confirmationKeys.current.clear();acknowledgementKeys.current.clear();setShots(undefined);setShotConflict(undefined);setPeople([]);setDraft([]);saveAttempt.current=null;setAlerts([]);setCursor(null);setError('');
  if(!id||!tenant){setError('capture_scope');setInitialLoading(false);return()=>controller.abort();}
  setBusy(true);
  const timeout=setTimeout(()=>{if(initialRead.current===controller&&!controller.signal.aborted){controller.abort();setInitialLoading(false);setBusy(false);setError('capture_load_error');}},8000);
  async function loadPeople(){
   const session=await api.shootSession(id,controller.signal);
   const access=await api.projectAccess(session.project_id,controller.signal);
   controller.signal.throwIfAborted();
   const found:CapturePerson[]=[];const seen=new Set<string>(),cursors=new Set<string>();let cursor:string|null=null;
   do{
    const page:Page<CapturePerson>=await api.capturePeople(session.project_id,cursor??undefined,controller.signal);
    if(controller.signal.aborted)throw controller.signal.reason;
    if(page.items.some(person=>seen.has(person.id))||new Set(page.items.map(person=>person.id)).size!==page.items.length||found.length+page.items.length>5000||cursors.size>=50)throw new Error('CAPTURE_PEOPLE_PAGINATION_INVALID');
    for(const person of page.items){seen.add(person.id);found.push(person);}
    const next=page.next_cursor??null;if(next&&(next===cursor||cursors.has(next)))throw new Error('CAPTURE_PEOPLE_PAGINATION_INVALID');
    if(next)cursors.add(next);cursor=next;
   }while(cursor&&!controller.signal.aborted);
   return {persons:found,session,canEdit:access.can_edit};
  }
  Promise.all([loadCaptureShots(api,id,controller.signal),api.captureAlerts(id,undefined,controller.signal),loadPeople(),api.get<{id:string}>('/api/v1/me',controller.signal)])
   .then(async([result,page,persons,user])=>{
    const store=new CaptureDraftStore(AsyncStorage,auth.origin,user.id,tenant,id);
    let local=null;
    try{local=await store.read()}catch{if(!controller.signal.aborted)setError('capture_draft_storage_error')}
    if(controller.signal.aborted)return;
    const active=new Set(persons.persons.filter(person=>person.state==='active').map(person=>person.id));
    if(result.blocked||local?.requirements.some(shot=>shot.person_ids.some(person=>!active.has(person)))){await store.clear().catch(()=>{});local=null;}
    if(controller.signal.aborted)return;
    draftStore.current=store;
    setProjectCanEdit(persons.canEdit);setReplaceBlocked(result.blocked);setSession(persons.session);setPeople(persons.persons);
    setShots(local?{...result.list,revision:local.revision}:result.list);setDraft(local?.requirements??result.list.requirements);
    if(local&&local.revision!==result.list.revision){setShotConflict(result.list);setError('capture_conflict')}
    setAlerts(page.items);setCursor(page.next_cursor??null);
   })
   .catch(error=>{if(!controller.signal.aborted&&!invalidateAccess(error))setError('capture_load_error')})
   .finally(()=>{clearTimeout(timeout);if(!controller.signal.aborted){setInitialLoading(false);setBusy(false);}});
  return()=>{clearTimeout(timeout);generation.current++;controller.abort();actionFlight.current?.abort();manualRead.current?.abort();if(initialRead.current===controller)initialRead.current=null;};
 },[api,id,tenant,refresh]);
 useEffect(()=>{
  if(!roleCanEdit){actionFlight.current?.abort();actionFlight.current=null;manualRead.current?.abort();manualRead.current=null;setProjectCanEdit(false);setBusy(false);return;}
  if(!session)return;
  const controller=new AbortController();
  void readDeadline(controller.signal,signal=>api.projectAccess(session.project_id,signal)).then(access=>{if(alive.current&&!controller.signal.aborted)setProjectCanEdit(access.can_edit);}).catch(error=>{if(!controller.signal.aborted){setProjectCanEdit(false);invalidateAccess(error);}});
  return()=>controller.abort();
 },[api,roleCanEdit,session?.project_id]);
 useEffect(()=>{
  const store=draftStore.current;if(!canEdit||!store||!shots)return;
  const scope=generation.current;
  const operation=replaceBlocked||JSON.stringify(draft)===JSON.stringify(shots.requirements)
   ?store.clear():store.write({version:1,revision:shots.revision,requirements:draft,saved_at:Date.now()});
  void operation.catch(()=>{if(scope===generation.current)setError('capture_draft_storage_error')});
 },[draft,shots,replaceBlocked,canEdit]);
 // Refresh alerts independently: never replace a photographer's unsaved draft.
 useEffect(()=>{
  if(!shots||busy||!id||!tenant)return;
  const controller=new AbortController();const scope=generation.current;
  let timer:ReturnType<typeof setTimeout>|undefined;
  let running=false;
  let streamTimer:ReturnType<typeof setTimeout>|undefined;let eventCursor:string|undefined;
  let streamRunning=false;let streamController:AbortController|undefined;
  const refreshQueue=new CoalescedRefresh(poll,controller.signal);
  const schedule=()=>{if(timer)clearTimeout(timer);if(!controller.signal.aborted)timer=setTimeout(()=>void refreshQueue.request(),5000)};
  async function poll(){
   if(controller.signal.aborted||running)return;
   if(AppState.currentState!=='active'){schedule();return;}
   running=true;
   try{
    const displayed=alertSnapshot.current;
    const delta=await readDeadline(controller.signal,next=>readCaptureAlertDelta((cursor,signal)=>api.captureAlerts(id,cursor,signal),displayed,next));
    if(controller.signal.aborted||scope!==generation.current)return;
    setAlerts(old=>mergeCaptureAlerts(old,delta.items));
    if(displayed.length===0)setCursor(delta.initialCursor);
   }catch(error){
    if(invalidateAccess(error,scope))return;
    // Keep displayed results and retry; foreground/manual reads report errors.
   }finally{running=false;schedule()}
  }
  const subscription=AppState.addEventListener('change',state=>{
   if(state!=='active')streamController?.abort();
   if(state==='active'){
    if(timer)clearTimeout(timer);void refreshQueue.request()
    if(!streamRunning){if(streamTimer)clearTimeout(streamTimer);void stream()}
   }
  });
  async function stream(){
   if(controller.signal.aborted||streamRunning)return;
   streamRunning=true;
   const connection=new AbortController();streamController=connection;
   const abort=()=>connection.abort();controller.signal.addEventListener('abort',abort,{once:true});
   try{
    if(AppState.currentState==='active')await api.streamEvents(eventCursor,event=>{
     eventCursor=event.cursor;
     const payload=event.payload as {session_id?:string}|null;
     if(event.type==='capture.alert'&&payload?.session_id===id){if(timer)clearTimeout(timer);void refreshQueue.request()}
    },connection.signal);
   }catch(error){
    if((error as {status?:number})?.status===410){eventCursor=undefined;if(timer)clearTimeout(timer);void refreshQueue.request()}
   }finally{
    controller.signal.removeEventListener('abort',abort);connection.abort();streamController=undefined;streamRunning=false;
    if(!controller.signal.aborted)streamTimer=setTimeout(()=>void stream(),2000)
   }
  }
  schedule();void stream();
  return()=>{controller.abort();if(timer)clearTimeout(timer);if(streamTimer)clearTimeout(streamTimer);subscription.remove()};
 },[api,id,tenant,busy,!!shots,roleCanEdit]);
 // Manual reads retain the shot-list draft and its original CAS revision.
 async function refreshView(){
  if(!active()||live.current.busy||actionFlight.current||manualRead.current||!id||!tenant)return;
  if(!shots){setRefresh(value=>value+1);return;}
  const controller=new AbortController();manualRead.current=controller;
  const displayed=alertSnapshot.current;
  const scope=generation.current;setBusy(true);setError('');
  try{
   const [latest,delta]=await readDeadline(controller.signal,signal=>Promise.all([
    api.shootSession(id,signal),
    readCaptureAlertDelta((cursor,signal)=>api.captureAlerts(id,cursor,signal),displayed,signal),
   ]));
   if(scope!==generation.current||controller.signal.aborted)return;
   setSession(latest);setAlerts(old=>mergeCaptureAlerts(old,delta.items));
   if(displayed.length===0)setCursor(delta.initialCursor);
  }catch(error){if(!controller.signal.aborted&&!invalidateAccess(error,scope)&&scope===generation.current)setError('capture_load_error')}
  finally{controller.abort();if(manualRead.current===controller){manualRead.current=null;if(scope===generation.current)setBusy(false)}}
 }
 useCommands({'capture.refresh':()=>void refreshView(),'capture.save':()=>void saveShots()});
 async function action(errorKey:string,work:(signal:AbortSignal)=>Promise<void>,milliseconds=15000){
  if(!active()||actionFlight.current||manualRead.current||live.current.busy)return;
  const controller=new AbortController(),scope=generation.current;actionFlight.current=controller;setBusy(true);setError('');
  try{await readDeadline(controller.signal,work,milliseconds);}
  catch(error){if(scope===generation.current&&!controller.signal.aborted&&!invalidateAccess(error,scope))setError(errorKey);}
  finally{controller.abort();if(actionFlight.current===controller){actionFlight.current=null;if(scope===generation.current)setBusy(false);}}
 }
 async function changeSessionState(state:'open'|'closed'){
  const session=live.current.session;if(!editable()||!session||session.state==='archived')return;
  const scope=generation.current;
  if(!sessionAttempt.current)sessionAttempt.current={body:JSON.stringify({expected_revision:session.revision,name:session.name,state,policy:session.policy,capture_policy:session.capture_policy}),key:crypto.randomUUID()};
  const frozen=sessionAttempt.current;
  await action('capture_state_error',async signal=>{
   const updated=await api.updateShootSession(id,JSON.parse(frozen.body),frozen.key,signal);signal.throwIfAborted();if(!active()||!live.current.canEdit||scope!==generation.current)return;
   setSession(updated);if(sessionAttempt.current===frozen)sessionAttempt.current=null;
  });
 }
 async function saveShots(){
  const {shots,draft,replaceBlocked,shotConflict}=live.current;if(!editable()||!shots||replaceBlocked||shotConflict)return;
  if(!saveAttempt.current&&draft.some(item=>!item.label.trim())){setError('capture_label_error');return;}
  const scope=generation.current;
  if(!saveAttempt.current)saveAttempt.current={body:JSON.stringify({revision:shots.revision,requirements:draft}),key:crypto.randomUUID()};
  const frozen=saveAttempt.current,request=JSON.parse(frozen.body) as {revision:string;requirements:CaptureShotRequirement[]};
  await action('capture_save_error',async signal=>{
   try{const saved=await api.replaceCaptureShotList(id,request.revision,request.requirements,frozen.key,signal);signal.throwIfAborted();if(!active()||!live.current.canEdit||scope!==generation.current)return;setShots(saved);setDraft(saved.requirements);if(saveAttempt.current===frozen)saveAttempt.current=null;}
   catch(error){
    signal.throwIfAborted();if(!active()||scope!==generation.current)return;
    if(error instanceof APIError&&error.status===409&&(error.code==='REVISION_CONFLICT'||error.code==='CONSENT_REVOKED')){
     const latest=await readDeadline(signal,next=>loadCaptureShots(api,id,next));signal.throwIfAborted();if(!active()||scope!==generation.current)return;
     if(latest.blocked){setReplaceBlocked(true);setShots(latest.list);setDraft([]);setShotConflict(undefined);saveAttempt.current=null;}
     else{setShotConflict(latest.list);setError('capture_conflict');}
    }else throw error;
   }
  });
 }
 async function more(){
  if(!active()||!cursor||live.current.busy||actionFlight.current||manualRead.current)return;const scope=generation.current,requested=cursor;
  await action('capture_more_error',async signal=>{const page=await api.captureAlerts(id,requested,signal);signal.throwIfAborted();if(!active()||scope!==generation.current)return;if(page.next_cursor===requested)throw new Error('CAPTURE_ALERT_CURSOR_REPEATED');setAlerts(old=>mergeCaptureAlerts(old,page.items));setCursor(page.next_cursor??null);},8000);
 }
 async function confirm(alert:CaptureAlert){
  if(!editable()||!alert.receipt_id)return;
  const scope=generation.current;let key=confirmationKeys.current.get(alert.id);
  if(!key){key=crypto.randomUUID();confirmationKeys.current.set(alert.id,key)}
  const frozenKey=key;
  await action('capture_confirm_error',async signal=>{const result=await api.confirmCaptureFrame(alert.receipt_id!,alert.id,frozenKey,signal);signal.throwIfAborted();if(!active()||!live.current.canEdit||scope!==generation.current)return;setAlerts(old=>mergeCaptureAlerts(old,[{...alert,confirmation_id:result.confirmation_id}]));});
 }
 async function acknowledge(alert:CaptureAlert){
  if(!editable()||alert.acknowledged_at)return;
  let pending=acknowledgementKeys.current.get(alert.id);
  if(!pending){pending={revision:alert.revision,key:crypto.randomUUID()};acknowledgementKeys.current.set(alert.id,pending)}
  const frozen=pending,scope=generation.current;
  await action('capture_ack_error',async signal=>{const updated=await api.acknowledgeCaptureAlert({...alert,revision:frozen.revision},frozen.key,signal);signal.throwIfAborted();if(!active()||!live.current.canEdit||scope!==generation.current)return;setAlerts(old=>mergeCaptureAlerts(old,[updated]));acknowledgementKeys.current.delete(alert.id);});
 }
 const displayedInitial=initialRead.current,displayedAction=actionFlight.current,displayedManual=manualRead.current;
 return <SafeAreaView style={s.root}><ScrollView contentContainerStyle={s.list}>
  <LocalePicker/>
  {initialLoading?<Button accessibilityRole="button"  onPress={()=>{if(!active()||!displayedInitial||initialRead.current!==displayedInitial||displayedInitial.signal.aborted)return;displayedInitial.abort();setInitialLoading(false);setBusy(false);setError('capture_load_error');}}   mode="contained" contentStyle={{minHeight:48}}>{t('read_cancel')}</Button>:null}
  <Text accessibilityRole="header" style={s.title}>{t("capture_session")}</Text>
  {session?<View><SessionAnalysisNavigation tenant={tenant} id={session.id} project={session.project_id} disabled={busy}/><Text style={s.meta}>{session.name} · {t(`capture_state_${session.state}`)}</Text>
   {session.state!=='archived'?<Button accessibilityRole="button" disabled={!canEdit||busy}  onPress={()=>void changeSessionState(session.state==='open'?'closed':'open')}   mode="contained" contentStyle={{minHeight:48}}>{t(session.state==='open'?'capture_close':'capture_reopen')}</Button>:null}
  </View>:null}
  <Button accessibilityRole="button" disabled={busy}  onPress={()=>void refreshView()}   mode="contained" contentStyle={{minHeight:48}}>{t(commands['capture.refresh'].label)}</Button>
  {error?<Text accessibilityRole="alert" style={s.error}>{t(error as Parameters<typeof t>[0])}</Text>:null}
  {busy?<><ActivityIndicator/>{actionFlight.current||manualRead.current?<Button accessibilityRole="button" onPress={()=>{if(!active()||(!displayedAction&&!displayedManual)||actionFlight.current!==displayedAction||manualRead.current!==displayedManual)return;displayedAction?.abort();actionFlight.current=null;displayedManual?.abort();manualRead.current=null;setBusy(false);setError('capture_save_error')}} mode="outlined">{t('read_cancel')}</Button>:null}</>:null}
  <Text style={s.name}>{t("capture_shots")}</Text>
  {replaceBlocked?<View><Text accessibilityRole="alert" style={s.error}>{t("capture_revoked")}</Text><Button accessibilityRole="button" disabled={!canEdit||busy}  onPress={()=>{if(editable()&&live.current.replaceBlocked)setReplaceBlocked(false);}}   mode="contained" contentStyle={{minHeight:48}}>{t("capture_replace")}</Button></View>:null}
  {shots&&!replaceBlocked?<><CaptureShotEditor people={people} items={draft} onChange={changeDraft} disabled={!canEdit||busy||!!saveAttempt.current}/>
  <Button accessibilityRole="button" disabled={!canEdit||busy||!!shotConflict}  onPress={()=>void saveShots()}   mode="contained" contentStyle={{minHeight:48}}>{t(commands['capture.save'].label)}</Button></>:null}
  {shotConflict?<View>
   <Text accessibilityRole="header" style={s.name}>{t('capture_server_shots')}</Text>
   <CaptureShotEditor people={people} items={shotConflict.requirements} onChange={()=>{}} disabled/>
   <Text style={s.meta}>{t('capture_conflict_choice')}</Text>
   <Button accessibilityRole="button" disabled={busy}  onPress={()=>{if(!editable()||live.current.shotConflict!==shotConflict)return;setShots(shotConflict);setDraft(shotConflict.requirements);setShotConflict(undefined);saveAttempt.current=null;setError('')}}   mode="contained" contentStyle={{minHeight:48}}>{t('capture_use_server')}</Button>
   <Button accessibilityRole="button" disabled={busy}  onPress={()=>{if(!editable()||live.current.shotConflict!==shotConflict)return;setShots(shotConflict);setShotConflict(undefined);saveAttempt.current=null;setError('')}}   mode="contained" contentStyle={{minHeight:48}}>{t('capture_keep_draft')}</Button>
  </View>:null}
  <Text style={s.name}>{t("capture_alerts")}</Text>
  {!busy&&alerts.length===0?<Text style={s.empty}>{t("capture_empty")}</Text>:null}
  {alerts.map(alert=><View key={alert.id} style={s.row}><View style={{flex:1}}>
   <Text style={s.name}>{t(`capture_${alert.severity}`)}</Text>
   <Text style={s.meta}>{t("capture_frame")} {alert.asset_version_id.slice(0,8)} · {alert.created_at}</Text>
   {alert.qa_stale?<Text accessibilityRole="alert" style={s.error}>{t("capture_delayed")}</Text>:null}
   {alert.receipt_id?(!!alert.confirmation_id?<Text style={s.meta}>{t("capture_confirmed")}</Text>:<Button accessibilityRole="button" disabled={!canEdit||busy}  onPress={()=>void confirm(alert)}   mode="contained" contentStyle={{minHeight:48}}>{t("capture_confirm")}</Button>):null}
   {alert.acknowledged_at?<Text style={s.meta}>{t("capture_acked")}</Text>:<Button accessibilityRole="button" disabled={!canEdit||busy}  onPress={()=>void acknowledge(alert)}   mode="contained" contentStyle={{minHeight:48}}>{t("capture_ack")}</Button>}
  </View></View>)}
  {cursor?<Button accessibilityRole="button" disabled={busy}  onPress={()=>void more()}   mode="contained" contentStyle={{minHeight:48}}>{t("capture_more_alerts")}</Button>:null}
 </ScrollView></SafeAreaView>;
}
