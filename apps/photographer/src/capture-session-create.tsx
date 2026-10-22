import {TouchableRipple,Button,ActivityIndicator,Text,TextInput} from 'react-native-paper';
import {readCaptureCollections} from './capture-list-contract';
import {APIError} from '@photoeditor/api-client';
import AsyncStorage from '@react-native-async-storage/async-storage';
import {CaptureCreateAttemptStore,type CaptureCreateAttempt} from './capture-create-attempt-store';
import {freezeCaptureCreate,readCreatedCapture} from './capture-create-contract';
import {readDeadline} from './read-deadline';
import {useEffect,useMemo,useRef,useState} from 'react';
import {View} from 'react-native';
import type {OwnerAPI} from './owner-api';
import type {ShootSession} from './capture-types';
import {useLocale} from './locale-context';
import {catalogStyle as s} from './catalog-style';
type CaptureSessionCreateProps={api:OwnerAPI;origin:string;project:string;sessions:Array<{id:string;name:string}>;onCreated:(session:ShootSession)=>void;onBusy:(busy:boolean)=>void};
export function CaptureSessionCreate(props:CaptureSessionCreateProps){
 const instance=useMemo(()=>crypto.randomUUID(),[props.api,props.origin,props.project]);
 return <CaptureSessionCreateScope key={instance} {...props}/>;
}
function CaptureSessionCreateScope({api,origin,project,sessions,onCreated,onBusy}:CaptureSessionCreateProps){
 const {t}=useLocale();
 const [name,setName]=useState(''),[collection,setCollection]=useState(''),[template,setTemplate]=useState('');
 const [collections,setCollections]=useState<Array<{id:string;name:string}>>([]);
 const [loading,setLoading]=useState(true),[busy,setBusy]=useState(false),[error,setError]=useState('');
 const [rejected,setRejected]=useState<string|null>(null),[retry,setRetry]=useState(0);const generation=useRef(0);const submitting=useRef(false);const action=useRef<AbortController|null>(null);const readFlight=useRef<AbortController|null>(null);
 const attempt=useRef<CaptureCreateAttempt|null>(null),journal=useRef<CaptureCreateAttemptStore|null>(null);
 const alive=useRef(true),live=useRef({name,collection,template,loading});live.current={name,collection,template,loading};
 useEffect(()=>{alive.current=true;return()=>{alive.current=false;};},[]);
 const editable=()=>alive.current&&!live.current.loading&&!submitting.current&&!attempt.current;
 const editName=(value:string)=>{if(editable())setName(value);};
 const editCollection=(value:string)=>{if(editable())setCollection(value);};
 const editTemplate=(value:string)=>{if(editable())setTemplate(value);};
 useEffect(()=>{
  const controller=new AbortController();readFlight.current=controller;generation.current++;setBusy(false);submitting.current=false;setLoading(true);setCollections([]);setCollection('');setTemplate('');attempt.current=null;journal.current=null;setRejected(null);setError('');
  async function load(signal:AbortSignal){
   const user=await readDeadline(signal,signal=>api.me(signal));signal.throwIfAborted();
   const store=new CaptureCreateAttemptStore(AsyncStorage,origin,user.id,api.tenant,project);journal.current=store;
   const saved=await readDeadline(signal,()=>store.read());signal.throwIfAborted();
   attempt.current=saved;if(saved){setName(saved.request.name);setCollection(saved.request.collection_id);setCollections([{id:saved.request.collection_id,name:saved.request.name}]);return;}

   const found:Array<{id:string;name:string}>=[];const ids=new Set<string>(),cursors=new Set<string>();let cursor:string|undefined;
   do{
    const page=readCaptureCollections(await readDeadline(signal,signal=>api.get<unknown>(`/api/v1/tenants/{t}/collections?limit=100&project_id=${encodeURIComponent(project)}${cursor?`&cursor=${encodeURIComponent(cursor)}`:''}`,signal)),api.tenant,project);
    for(const item of page.items)if(!ids.has(item.id)){ids.add(item.id);found.push(item)}
    cursor=page.next_cursor??undefined;
    if(found.length>5000||cursors.size>=50)throw new Error('Collection inventory limit');if(cursor){if(cursors.has(cursor))throw new Error('Repeated collection cursor');cursors.add(cursor)}
   }while(cursor&&!signal.aborted);
   if(!signal.aborted)setCollections(found);
  }
  void readDeadline(controller.signal,signal=>load(signal)).catch(()=>{if(!controller.signal.aborted)setError('capture_collections_error')}).finally(()=>{if(!controller.signal.aborted)setLoading(false)});
  return()=>{generation.current++;controller.abort();action.current?.abort();action.current=null;submitting.current=false;onBusy(false)};
 },[api,origin,project,retry]);
 async function create(){
  const draft=live.current;
  if(!alive.current||submitting.current||draft.loading||!journal.current||!attempt.current&&(!draft.name.trim()||!draft.collection||!draft.template))return;
  submitting.current=true;setBusy(true);onBusy(true);setError('');const scope=generation.current;
  const controller=new AbortController();action.current=controller;const current=()=>alive.current&&scope===generation.current&&action.current===controller&&!controller.signal.aborted;const frozen={name:draft.name.trim(),collection:draft.collection,template:draft.template};
  let phase:'prepare'|'submit'|'read'='prepare';
  try{
   if(!attempt.current){
    const source=await readDeadline(controller.signal,signal=>api.shootSession(frozen.template,signal));
    if(!current())return;
    if(source.project_id!==project||!source.capture_policy){setError('capture_template_unavailable');return;}
    const advisory=await readDeadline(controller.signal,signal=>api.captureCapabilities(source,signal));if(!current())return;if(!advisory.available){setError('capability_unsupported');return;}
    attempt.current={schema_version:1,key:crypto.randomUUID(),ticket:null,request:freezeCaptureCreate({project_id:project,collection_id:frozen.collection,name:frozen.name,policy:source.policy,capture_policy:source.capture_policy})};
   }
   let pending=attempt.current;const store=journal.current!;
   await readDeadline(controller.signal,()=>store.write(pending!));if(!current())return;
   if(!pending.ticket){
    phase='submit';const created=readCreatedCapture(await readDeadline(controller.signal,signal=>api.createShootSession(pending!.request,pending!.key,signal),15000),pending.request,undefined,true);if(!current())return;
    pending={...pending,ticket:{resource_id:created.id}};attempt.current=pending;
    await readDeadline(controller.signal,()=>store.write(pending!));if(!current())return;
   }
   if(!pending.ticket)throw new Error('CAPTURE_RECEIPT_REQUIRED');const acceptedTicket=pending.ticket;
   phase='read';const saved=readCreatedCapture(await readDeadline(controller.signal,signal=>api.shootSession(pending!.ticket!.resource_id,signal)),pending.request,acceptedTicket.resource_id);if(!current())return;
   onCreated(saved);if(!current())return;
   await readDeadline(controller.signal,()=>store.clearAccepted(pending!.key,pending!.ticket!.resource_id));if(!current())return;
   attempt.current=null;
  }catch(cause){if(current()){if(phase==='submit'&&!attempt.current?.ticket&&cause instanceof APIError&&(cause.status===422&&cause.code==='VALIDATION_FAILED'||cause.status===409&&cause.code==='REVISION_CONFLICT'))setRejected(attempt.current?.key??null);setError('capture_create_error')}}
  finally{if(action.current===controller){action.current=null;submitting.current=false;if(scope===generation.current){setBusy(false);onBusy(false)}}}
 }
 async function discardRejected(){
  const pending=attempt.current,store=journal.current;if(!alive.current||!pending||pending.ticket||pending.key!==rejected||!store||submitting.current)return;
  const controller=new AbortController();action.current=controller;submitting.current=true;setBusy(true);onBusy(true);const scope=generation.current;
  const current=()=>alive.current&&scope===generation.current&&action.current===controller&&!controller.signal.aborted;
  try{await readDeadline(controller.signal,()=>store.discardRejected(pending.key));if(!current())return;attempt.current=null;setRejected(null);setRetry(value=>value+1);}catch{if(current())setError('capture_create_error')}finally{if(action.current===controller){action.current=null;submitting.current=false;if(scope===generation.current){setBusy(false);onBusy(false)}}}
 }
 const displayedRead=readFlight.current,displayedAction=action.current;
 return <View style={{gap:8}}>
  <Text accessibilityRole="header" style={s.name}>{t('capture_create_session')}</Text>
  <Text style={s.meta}>{t('capture_create_description')}</Text>
  <TextInput accessibilityLabel={t('capture_session_name')} value={name} onChangeText={editName} maxLength={200} editable={!busy&&!attempt.current} style={s.input}/>
  <Text style={s.name}>{t('capture_choose_collection')}</Text>
  {loading?<ActivityIndicator/>:collections.map(item=><TouchableRipple key={item.id} accessibilityRole="radio" aria-checked={collection===item.id} accessibilityState={{checked:collection===item.id,disabled:busy||!!attempt.current}} disabled={busy||!!attempt.current} style={s.button} onPress={()=>editCollection(item.id)}><View style={{alignItems:'center',justifyContent:'center'}}><Text style={s.buttonText}>{collection===item.id?'✓ ':''}{item.name}</Text></View></TouchableRipple>)}
  {!loading&&!collections.length?<Text style={s.meta}>{t('capture_no_collections')}</Text>:null}
  <Text style={s.name}>{t('capture_choose_template')}</Text>
  {sessions.map(item=><TouchableRipple key={item.id} accessibilityRole="radio" aria-checked={template===item.id} accessibilityState={{checked:template===item.id,disabled:busy||!!attempt.current}} disabled={busy||!!attempt.current} style={s.button} onPress={()=>editTemplate(item.id)}><View style={{alignItems:'center',justifyContent:'center'}}><Text style={s.buttonText}>{template===item.id?'✓ ':''}{item.name}</Text></View></TouchableRipple>)}
  {!sessions.length?<Text style={s.meta}>{t('capture_no_template')}</Text>:null}
  {error?<Text accessibilityRole="alert" style={s.error}>{t(error as Parameters<typeof t>[0])}</Text>:null}
  {error==='capture_collections_error'?<Button accessibilityRole="button" disabled={busy} onPress={()=>setRetry(value=>value+1)}    mode="contained" contentStyle={{minHeight:48}}>{t('capture_refresh')}</Button>:null}
  {rejected&&attempt.current?.key===rejected?<Button accessibilityRole="button" disabled={busy}  onPress={()=>void discardRejected()}   mode="contained" contentStyle={{minHeight:48}}>{t('session_draft_discard')}</Button>:null}
  {loading?<Button accessibilityRole="button"  onPress={()=>{if(!alive.current||!displayedRead||readFlight.current!==displayedRead||displayedRead.signal.aborted)return;displayedRead.abort();setLoading(false);setError('capture_collections_error')}}   mode="contained" contentStyle={{minHeight:48}}>{t('read_cancel')}</Button>:null}
  {busy?<Button accessibilityRole="button"  onPress={()=>{if(!alive.current||!displayedAction||action.current!==displayedAction||displayedAction.signal.aborted)return;displayedAction.abort();action.current=null;submitting.current=false;setBusy(false);onBusy(false);setError('capture_create_error')}}   mode="contained" contentStyle={{minHeight:48}}>{t('read_cancel')}</Button>:null}
  <Button accessibilityRole="button" disabled={busy||loading||!attempt.current&&(!name.trim()||!collection||!template)}  onPress={()=>void create()}   mode="contained" contentStyle={{minHeight:48}}>{t('capture_create_session')}</Button>
 </View>;
}
