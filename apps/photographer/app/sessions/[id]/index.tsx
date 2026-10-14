import AsyncStorage from '@react-native-async-storage/async-storage';
import {revokeSessionPolicyDrafts} from '../../../src/session-policy-draft';
import {SessionLifecycle} from '../../../src/session-lifecycle';
import {PersistentSessionPolicyEditor} from '../../../src/persistent-session-policy-editor';
import type {ShootSessionUpdate} from '../../../src/capture-types';
import {initialSceneAnalysisRequest,type SceneAnalysisRequest} from '../../../src/scene-analysis-request';
import {useEffect,useMemo,useRef,useState} from 'react';
import {router,useLocalSearchParams} from 'expo-router';
import {APIError} from '@photoeditor/api-client';
import {ActivityIndicator,FlatList,Pressable,SafeAreaView,Text,View} from 'react-native';
import {useAuth} from '../../../src/auth-context';
import {useLocale} from '../../../src/locale-context';
import {LocalePicker} from '../../../src/locale-picker';
import {OwnerAPI} from '../../../src/owner-api';
import type {ShootSession} from '../../../src/capture-types';
import type {SessionAnalysisSummary} from '../../../src/session-analysis-list';
import {catalogStyle as s} from '../../../src/catalog-style';
export default function SessionPage(){
 const {id,tenant}=useLocalSearchParams<{id:string;tenant:string}>();const auth=useAuth();
 const api=useMemo(()=>new OwnerAPI(auth.client(),tenant||''),[auth,tenant]);
 return <SessionScope key={JSON.stringify([id,tenant])} id={id} api={api} origin={auth.origin}/>;
}
function SessionScope({id,api,origin}:{id:string;api:OwnerAPI;origin:string}){
 const {t}=useLocale();
 function revokeDraft(){void revokeSessionPolicyDrafts(AsyncStorage,origin,api.tenant,id).catch(()=>setError('scene_access_denied'));}
 const lifetime=useRef<AbortController|null>(null),loading=useRef(false),cursors=useRef(new Set<string>());
 const [policyDirty,setPolicyDirty]=useState(false),[statePending,setStatePending]=useState(false);
 const attempt=useRef<{request:SceneAnalysisRequest;key:string}|null>(null);
 const [reload,setReload]=useState(0),[busy,setBusy]=useState(true),[error,setError]=useState<'session_load_failed'|'scene_reference_failed'|'scene_reference_conflict'|'scene_reference_unavailable'|'scene_access_denied'|null>(null);
 const [loaded,setLoaded]=useState<{api:OwnerAPI;session:ShootSession;items:SessionAnalysisSummary[];cursor:string|null}>();
 useEffect(()=>{
  const controller=new AbortController();lifetime.current=controller;loading.current=false;attempt.current=null;cursors.current.clear();setLoaded(undefined);setBusy(true);setError(null);
  async function load(){try{if(!id||!api.tenant)throw new Error('SESSION_SCOPE');const [session,page]=await Promise.all([api.shootSession(id,controller.signal),api.sessionAnalyses(id,undefined,controller.signal)]);if(!controller.signal.aborted)setLoaded({api,session,items:page.items,cursor:page.next_cursor??null});}catch(error){if(!controller.signal.aborted){if(error instanceof APIError&&[401,403,404].includes(error.status))revokeDraft();setError('session_load_failed');}}finally{if(!controller.signal.aborted)setBusy(false);}}
  void load();return()=>{controller.abort();if(lifetime.current===controller)lifetime.current=null;};
 },[id,api,reload]);
 async function more(){
  const controller=lifetime.current;if(!loaded||loaded.api!==api||!loaded.cursor||!controller||controller.signal.aborted||loading.current)return;
  loading.current=true;setBusy(true);setError(null);const cursor=loaded.cursor;
  try{
   const page=await api.sessionAnalyses(id,cursor,controller.signal);if(controller.signal.aborted)return;
   if(cursors.current.has(cursor)||page.next_cursor===cursor||page.items.some(item=>loaded.items.some(previous=>previous.id===item.id))||loaded.items.length+page.items.length>5000)throw new Error('SESSION_PAGINATION_INVALID');
   cursors.current.add(cursor);setLoaded({...loaded,items:[...loaded.items,...page.items],cursor:page.next_cursor??null});
  }catch(error){if(!controller.signal.aborted){if(error instanceof APIError&&[401,403,404].includes(error.status)){revokeDraft();setLoaded(undefined);}setError('session_load_failed');}}
  finally{if(!controller.signal.aborted){loading.current=false;setBusy(false);}}
 }
 async function readCurrentPolicy(){
  const controller=lifetime.current;
  if(!controller||controller.signal.aborted)throw new Error('SESSION_CANCELLED');
  try{const current=await api.shootSession(id,controller.signal);if(controller.signal.aborted||lifetime.current!==controller)throw new Error('SESSION_CANCELLED');return current;}
  catch(error){if(!controller.signal.aborted&&error instanceof APIError&&[401,403,404].includes(error.status)){revokeDraft();setLoaded(undefined);setError('scene_access_denied');}throw error;}
 }
 async function savePolicy(request:ShootSessionUpdate,key:string){
  const controller=lifetime.current;
  if(!loaded||loaded.api!==api||!controller||controller.signal.aborted||loading.current||attempt.current)throw new Error('SESSION_BUSY');
  loading.current=true;setBusy(true);setError(null);
  try{
   const session=await api.updateShootSession(id,request,key,controller.signal);
   if(controller.signal.aborted||lifetime.current!==controller)throw new Error('SESSION_CANCELLED');
   setLoaded({...loaded,session:BigInt(session.revision)<BigInt(loaded.session.revision)?loaded.session:session});setPolicyDirty(false);
  }catch(error){if(!controller.signal.aborted){if(error instanceof APIError&&[401,403,404].includes(error.status)){revokeDraft();setLoaded(undefined);}setError(error instanceof APIError&&[409,412].includes(error.status)?'scene_reference_conflict':'session_load_failed');}throw error;}
  finally{if(!controller.signal.aborted){loading.current=false;setBusy(false);}}
 }
 async function analyze(){
  const controller=lifetime.current;if(!loaded||loaded.api!==api||!controller||controller.signal.aborted||loading.current)return;
  loading.current=true;setBusy(true);setError(null);
  try{
   if(!attempt.current){
    if(policyDirty||loaded.session.state!=='open')throw new Error('SESSION_CLOSED');
    const collection=await api.collection(loaded.session.collection_id);if(controller.signal.aborted)return;
    const snapshot=await api.selectionSnapshot(collection);if(controller.signal.aborted)return;
    attempt.current={request:initialSceneAnalysisRequest(loaded.session,snapshot.id),key:crypto.randomUUID()};
   }
   const ticket=await api.analyzeSession(id,attempt.current.request,attempt.current.key,controller.signal);
   if(controller.signal.aborted||lifetime.current!==controller)return;
   attempt.current=null;router.push({pathname:'/runs/[id]',params:{id:ticket.run_id,tenant:api.tenant}});
  }catch(error){if(!controller.signal.aborted){
   if(error instanceof APIError&&[401,403,404].includes(error.status)){revokeDraft();attempt.current=null;setLoaded(undefined);setError('scene_access_denied');}
   else if(error instanceof APIError&&[409,410,412,422].includes(error.status)){attempt.current=null;setError('scene_reference_conflict');}
   else setError(error instanceof APIError&&error.status===503?'scene_reference_unavailable':'scene_reference_failed');
  }}finally{if(!controller.signal.aborted){loading.current=false;setBusy(false);}}
 }
 const data=loaded?.api===api?loaded:undefined;
 return <SafeAreaView style={s.root}><View style={s.header}><LocalePicker/><Text accessibilityRole="header" style={s.title}>{data?.session.name||t('session_title')}</Text><Pressable accessibilityRole="button" disabled={busy||policyDirty||statePending||!!attempt.current} onPress={()=>{if(!loading.current&&!attempt.current)setReload(value=>value+1);}} style={s.button}><Text style={s.buttonText}>{t('scene_reload')}</Text></Pressable>{error?<Text accessibilityRole="alert" style={s.error}>{t(error)}</Text>:null}</View>
  {data?<SessionLifecycle key={"state:"+data.session.revision} session={data.session} disabled={busy||policyDirty||!!attempt.current} onSave={savePolicy} onPending={setStatePending}/>:null}
  {data?<PersistentSessionPolicyEditor api={api} origin={origin} key={data.session.revision} session={data.session} disabled={busy||statePending||!!attempt.current} onReadCurrent={readCurrentPolicy} onUseCurrent={current=>{if(lifetime.current&&!lifetime.current.signal.aborted&&loaded?.api===api&&current.id===id){setLoaded({...loaded,session:current});setPolicyDirty(false);}}} onSave={savePolicy} onDirty={setPolicyDirty}/>:null}
  {data?.session.state==='open'?<Pressable accessibilityRole="button" disabled={busy||policyDirty||statePending} onPress={()=>void analyze()} style={s.button}><Text style={s.buttonText}>{t(attempt.current?'scene_retry_analysis':'session_analyze')}</Text></Pressable>:null}
  {busy?<ActivityIndicator/>:null}<FlatList data={data?.items??[]} keyExtractor={item=>item.id} initialNumToRender={10} windowSize={5} ListEmptyComponent={!busy?<Text style={s.empty}>{t('session_no_analyses')}</Text>:null}
   renderItem={({item,index})=><Pressable accessibilityRole="button" onPress={()=>router.push({pathname:'/sessions/[id]/scenes',params:{id,tenant:api.tenant,analysis:item.id}})} style={s.button}><Text style={s.buttonText}>{t('session_analysis')} {index+1}</Text><Text style={s.meta}>{item.createdAt}</Text></Pressable>}
   ListFooterComponent={data?.cursor?<Pressable accessibilityRole="button" disabled={busy||!!attempt.current} onPress={()=>void more()} style={s.button}><Text style={s.buttonText}>{t('session_more')}</Text></Pressable>:null}/>
 </SafeAreaView>;
}
