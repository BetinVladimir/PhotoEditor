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
import {useLocalSearchParams} from 'expo-router';
import {AppState,ActivityIndicator,Pressable,SafeAreaView,ScrollView,Text,View} from 'react-native';
import {useAuth} from '../../src/auth-context';
import {OwnerAPI} from '../../src/owner-api';
import type {ShootSession,CaptureAlert,CaptureShotListView,CaptureShotRequirement,CapturePerson} from '../../src/capture-types';
import {catalogStyle as s} from '../../src/catalog-style';

export default function CaptureSession(){
 const {id,tenant}=useLocalSearchParams<{id:string;tenant:string}>();
 const {t}=useLocale();
 const auth=useAuth();
 const api=useMemo(()=>new OwnerAPI(auth.client(),tenant||''),[auth,tenant]);
 const generation=useRef(0);
 const draftStore=useRef<CaptureDraftStore|null>(null);
 const manualRead=useRef<AbortController|null>(null);
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
 function invalidateAccess(error:unknown,scope=generation.current){
  if(scope!==generation.current||!(error instanceof APIError)||![401,403,404].includes(error.status))return false;
  generation.current++;manualRead.current?.abort();draftStore.current=null;
  setShots(undefined);setShotConflict(undefined);setDraft([]);setPeople([]);setAlerts([]);setSession(undefined);setCursor(null);setBusy(false);setError('capture_access_denied');
  const clearedScope=generation.current;
  void clearCaptureSessionDrafts(AsyncStorage,auth.origin,tenant,id).catch(()=>{if(clearedScope===generation.current)setError('capture_draft_storage_error')});
  return true;
 }
 useEffect(()=>{
  const controller=new AbortController();generation.current++;draftStore.current=null;
  setSession(undefined);sessionAttempt.current=null;setReplaceBlocked(false);confirmationKeys.current.clear();acknowledgementKeys.current.clear();setShots(undefined);setShotConflict(undefined);setPeople([]);setDraft([]);saveAttempt.current=null;setAlerts([]);setCursor(null);setError('');
  if(!id||!tenant){setError('capture_scope');return;}
  setBusy(true);
  async function loadPeople(){
   const session=await api.shootSession(id,controller.signal);
   const found:CapturePerson[]=[];const seen=new Set<string>();let cursor:string|null=null;
   do{
    const page:Page<CapturePerson>=await api.capturePeople(session.project_id,cursor??undefined,controller.signal);
    for(const person of page.items)if(!seen.has(person.id)){seen.add(person.id);found.push(person)}
    cursor=page.next_cursor??null;
   }while(cursor&&!controller.signal.aborted);
   return {persons:found,session};
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
    setReplaceBlocked(result.blocked);setSession(persons.session);setPeople(persons.persons);
    setShots(local?{...result.list,revision:local.revision}:result.list);setDraft(local?.requirements??result.list.requirements);
    if(local&&local.revision!==result.list.revision){setShotConflict(result.list);setError('capture_conflict')}
    setAlerts(page.items);setCursor(page.next_cursor??null);
   })
   .catch(error=>{if(!controller.signal.aborted&&!invalidateAccess(error))setError('capture_load_error')})
   .finally(()=>{if(!controller.signal.aborted)setBusy(false)});
  return()=>{generation.current++;controller.abort();manualRead.current?.abort()};
 },[api,id,tenant,refresh]);
 useEffect(()=>{
  const store=draftStore.current;if(!store||!shots)return;
  const scope=generation.current;
  const operation=replaceBlocked||JSON.stringify(draft)===JSON.stringify(shots.requirements)
   ?store.clear():store.write({version:1,revision:shots.revision,requirements:draft,saved_at:Date.now()});
  void operation.catch(()=>{if(scope===generation.current)setError('capture_draft_storage_error')});
 },[draft,shots,replaceBlocked]);
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
    const delta=await readCaptureAlertDelta((cursor,signal)=>api.captureAlerts(id,cursor,signal),displayed,controller.signal);
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
 },[api,id,tenant,busy,!!shots]);
 // Manual reads retain the shot-list draft and its original CAS revision.
 async function refreshView(){
  if(busy||!id||!tenant)return;
  if(!shots){setRefresh(value=>value+1);return;}
  const controller=new AbortController();manualRead.current=controller;
  const displayed=alertSnapshot.current;
  const scope=generation.current;setBusy(true);setError('');
  try{
   const [latest,delta]=await Promise.all([
    api.shootSession(id,controller.signal),
    readCaptureAlertDelta((cursor,signal)=>api.captureAlerts(id,cursor,signal),displayed,controller.signal),
   ]);
   if(scope!==generation.current)return;
   setSession(latest);setAlerts(old=>mergeCaptureAlerts(old,delta.items));
   if(displayed.length===0)setCursor(delta.initialCursor);
  }catch(error){if(!invalidateAccess(error,scope)&&scope===generation.current)setError('capture_load_error')}
  finally{controller.abort();if(manualRead.current===controller)manualRead.current=null;if(scope===generation.current)setBusy(false)}
 }
 useCommands({'capture.refresh':()=>void refreshView(),'capture.save':()=>void saveShots()});
 async function changeSessionState(state:'open'|'closed'){
  if(!session||busy||session.state==='archived')return;
  const scope=generation.current;
  const request={expected_revision:session.revision,name:session.name,state,policy:session.policy,capture_policy:session.capture_policy};
  const body=JSON.stringify(request);
  if(sessionAttempt.current?.body!==body)sessionAttempt.current={body,key:crypto.randomUUID()};
  setBusy(true);setError('');
  try{
   const updated=await api.updateShootSession(id,request,sessionAttempt.current.key);
   if(scope!==generation.current)return;
   setSession(updated);sessionAttempt.current=null;
  }catch{if(scope===generation.current)setError('capture_state_error')}
  finally{if(scope===generation.current)setBusy(false)}
 }
 async function saveShots(){
  if(!shots||busy||replaceBlocked||shotConflict)return;
  if(draft.some(item=>!item.label.trim())){setError('capture_label_error');return;}
  const scope=generation.current;
  const body=JSON.stringify({revision:shots.revision,requirements:draft});
  if(saveAttempt.current?.body!==body)saveAttempt.current={body,key:crypto.randomUUID()};
  const key=saveAttempt.current.key;setBusy(true);setError('');
  try{const saved=await api.replaceCaptureShotList(id,shots.revision,draft,key);if(scope!==generation.current)return;setShots(saved);setDraft(saved.requirements);saveAttempt.current=null}
  catch(error){
   if(scope!==generation.current)return;
   if(error instanceof APIError&&error.status===409&&(error.code==='REVISION_CONFLICT'||error.code==='CONSENT_REVOKED')){
    try{
     const latest=await loadCaptureShots(api,id);
     if(scope!==generation.current)return;
     if(latest.blocked){setReplaceBlocked(true);setShots(latest.list);setDraft([]);setShotConflict(undefined);saveAttempt.current=null;}
     else{setShotConflict(latest.list);setError('capture_conflict');}
    }catch(error){if(!invalidateAccess(error,scope)&&scope===generation.current)setError('capture_conflict_load_error')}
   }else setError('capture_save_error');
  }
  finally{if(scope===generation.current)setBusy(false)}
 }
 async function more(){
  if(!cursor||busy)return;const scope=generation.current;setBusy(true);setError('');
  try{const page=await api.captureAlerts(id,cursor);if(scope!==generation.current)return;setAlerts(old=>mergeCaptureAlerts(old,page.items));setCursor(page.next_cursor??null)}
  catch{if(scope===generation.current)setError('capture_more_error')}finally{if(scope===generation.current)setBusy(false)}
 }
 async function confirm(alert:CaptureAlert){
  if(busy||!alert.receipt_id)return;
  const scope=generation.current;let key=confirmationKeys.current.get(alert.id);
  if(!key){key=crypto.randomUUID();confirmationKeys.current.set(alert.id,key)}
  setBusy(true);setError('');
  try{const result=await api.confirmCaptureFrame(alert.receipt_id,alert.id,key);if(scope!==generation.current)return;setAlerts(old=>mergeCaptureAlerts(old,[{...alert,confirmation_id:result.confirmation_id}]))}
  catch{if(scope===generation.current)setError('capture_confirm_error')}
  finally{if(scope===generation.current)setBusy(false)}
 }
 async function acknowledge(alert:CaptureAlert){
  if(busy||alert.acknowledged_at)return;
  let attempt=acknowledgementKeys.current.get(alert.id);
  if(attempt?.revision!==alert.revision){attempt={revision:alert.revision,key:crypto.randomUUID()};acknowledgementKeys.current.set(alert.id,attempt)}
  const scope=generation.current;setBusy(true);setError('');
  try{const updated=await api.acknowledgeCaptureAlert(alert,attempt.key);if(scope!==generation.current)return;setAlerts(old=>mergeCaptureAlerts(old,[updated]))}
  catch{if(scope===generation.current)setError('capture_ack_error')}
  finally{if(scope===generation.current)setBusy(false)}
 }
 return <SafeAreaView style={s.root}><ScrollView contentContainerStyle={s.list}>
  <LocalePicker/>
  <Text accessibilityRole="header" style={s.title}>{t("capture_session")}</Text>
  {session?<View><Text style={s.meta}>{session.name} · {t(`capture_state_${session.state}`)}</Text>
   {session.state!=='archived'?<Pressable accessibilityRole="button" disabled={busy} style={s.button} onPress={()=>void changeSessionState(session.state==='open'?'closed':'open')}><Text style={s.buttonText}>{t(session.state==='open'?'capture_close':'capture_reopen')}</Text></Pressable>:null}
  </View>:null}
  <Pressable accessibilityRole="button" disabled={busy} style={s.button} onPress={()=>void refreshView()}><Text style={s.buttonText}>{t(commands['capture.refresh'].label)}</Text></Pressable>
  {error?<Text accessibilityRole="alert" style={s.error}>{t(error as Parameters<typeof t>[0])}</Text>:null}
  {busy?<ActivityIndicator/>:null}
  <Text style={s.name}>{t("capture_shots")}</Text>
  {replaceBlocked?<View><Text accessibilityRole="alert" style={s.error}>{t("capture_revoked")}</Text><Pressable accessibilityRole="button" disabled={busy} style={s.button} onPress={()=>setReplaceBlocked(false)}><Text style={s.buttonText}>{t("capture_replace")}</Text></Pressable></View>:null}
  {shots&&!replaceBlocked?<><CaptureShotEditor people={people} items={draft} onChange={setDraft} disabled={busy}/>
  <Pressable accessibilityRole="button" disabled={busy||!!shotConflict} style={s.button} onPress={()=>void saveShots()}><Text style={s.buttonText}>{t(commands['capture.save'].label)}</Text></Pressable></>:null}
  {shotConflict?<View>
   <Text accessibilityRole="header" style={s.name}>{t('capture_server_shots')}</Text>
   <CaptureShotEditor people={people} items={shotConflict.requirements} onChange={()=>{}} disabled/>
   <Text style={s.meta}>{t('capture_conflict_choice')}</Text>
   <Pressable accessibilityRole="button" disabled={busy} style={s.button} onPress={()=>{setShots(shotConflict);setDraft(shotConflict.requirements);setShotConflict(undefined);saveAttempt.current=null;setError('')}}><Text style={s.buttonText}>{t('capture_use_server')}</Text></Pressable>
   <Pressable accessibilityRole="button" disabled={busy} style={s.button} onPress={()=>{setShots(shotConflict);setShotConflict(undefined);saveAttempt.current=null;setError('')}}><Text style={s.buttonText}>{t('capture_keep_draft')}</Text></Pressable>
  </View>:null}
  <Text style={s.name}>{t("capture_alerts")}</Text>
  {!busy&&alerts.length===0?<Text style={s.empty}>{t("capture_empty")}</Text>:null}
  {alerts.map(alert=><View key={alert.id} style={s.row}><View style={{flex:1}}>
   <Text style={s.name}>{t(`capture_${alert.severity}`)}</Text>
   <Text style={s.meta}>{t("capture_frame")} {alert.asset_version_id.slice(0,8)} · {alert.created_at}</Text>
   {alert.qa_stale?<Text accessibilityRole="alert" style={s.error}>{t("capture_delayed")}</Text>:null}
   {alert.receipt_id?(!!alert.confirmation_id?<Text style={s.meta}>{t("capture_confirmed")}</Text>:<Pressable accessibilityRole="button" disabled={busy} style={s.button} onPress={()=>void confirm(alert)}><Text style={s.buttonText}>{t("capture_confirm")}</Text></Pressable>):null}
   {alert.acknowledged_at?<Text style={s.meta}>{t("capture_acked")}</Text>:<Pressable accessibilityRole="button" disabled={busy} style={s.button} onPress={()=>void acknowledge(alert)}><Text style={s.buttonText}>{t("capture_ack")}</Text></Pressable>}
  </View></View>)}
  {cursor?<Pressable accessibilityRole="button" disabled={busy} style={s.button} onPress={()=>void more()}><Text style={s.buttonText}>{t("capture_more_alerts")}</Text></Pressable>:null}
 </ScrollView></SafeAreaView>;
}
