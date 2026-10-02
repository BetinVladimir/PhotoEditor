import {APIError} from '@photoeditor/api-client';
import {sceneReferenceRequest,type SceneAnalysisRequest} from '../../../src/scene-analysis-request';
import {useEffect,useMemo,useRef,useState} from 'react';
import {router,useLocalSearchParams} from 'expo-router';
import {ActivityIndicator,Pressable,SafeAreaView,Text,View} from 'react-native';
import {useAuth} from '../../../src/auth-context';
import {useLocale} from '../../../src/locale-context';
import {LocalePicker} from '../../../src/locale-picker';
import {OwnerAPI} from '../../../src/owner-api';
import type {SceneBoard} from '../../../src/scene-board';
import {SceneBoardView} from '../../../src/scene-board-view';
import {catalogStyle as s} from '../../../src/catalog-style';
export default function SessionScenes(){
 const {id,tenant,analysis}=useLocalSearchParams<{id:string;tenant:string;analysis:string}>();
 const auth=useAuth();const api=useMemo(()=>new OwnerAPI(auth.client(),tenant||''),[auth,tenant]);
 return <SceneScope key={JSON.stringify([id,tenant,analysis])} id={id} analysis={analysis} api={api}/>;
}
type SceneError='scene_load_failed'|'scene_reference_failed'|'scene_reference_conflict'|'scene_reference_unavailable'|'scene_access_denied';
function SceneScope({id,analysis,api}:{id:string;analysis:string;api:OwnerAPI}){
 const lifetime=useRef<AbortController|null>(null),lock=useRef(false);
 const attempt=useRef<{request:SceneAnalysisRequest;key:string}|null>(null);
 const [submitting,setSubmitting]=useState(false);
 const {t}=useLocale();const [reload,setReload]=useState(0);
 const [loaded,setLoaded]=useState<{api:OwnerAPI;reload:number;board:SceneBoard}>();const [busy,setBusy]=useState(true),[error,setError]=useState<SceneError|null>(null);
 useEffect(()=>{
  const controller=new AbortController();lifetime.current=controller;attempt.current=null;setLoaded(undefined);setError(null);setBusy(true);
  async function load(){try{if(!id||!analysis||!api.tenant)throw new Error('SCENE_SCOPE_INVALID');const board=await api.sceneBoard(analysis,id,controller.signal);if(!controller.signal.aborted)setLoaded({api,reload,board});}catch{if(!controller.signal.aborted)setError('scene_load_failed');}finally{if(!controller.signal.aborted)setBusy(false);}}
  void load();return()=>{controller.abort();if(lifetime.current===controller)lifetime.current=null;};
 },[id,analysis,api,reload]);
 async function submitReference(groupId?:string,versionId?:string){
  const controller=lifetime.current;
  if(lock.current||!controller||controller.signal.aborted||!loaded)return;
  lock.current=true;setSubmitting(true);setError(null);
  try{
   if(!attempt.current){
    if(!groupId||!versionId)throw new Error('SCENE_REFERENCE_MISSING');
    const session=await api.shootSession(id,controller.signal);
    if(controller.signal.aborted)return;
    const collection=await api.collection(session.collection_id);
    if(controller.signal.aborted)return;
    const snapshot=await api.selectionSnapshot(collection);
    if(controller.signal.aborted)return;
    attempt.current={request:sceneReferenceRequest(loaded.board,analysis,session,snapshot.id,groupId,versionId),key:crypto.randomUUID()};
   }
   const ticket=await api.analyzeSession(id,attempt.current.request,attempt.current.key,controller.signal);
   if(controller.signal.aborted||lifetime.current!==controller)return;
   attempt.current=null;
   router.push({pathname:'/runs/[id]',params:{id:ticket.run_id,tenant:api.tenant}});
  }catch(error){
   if(!controller.signal.aborted&&lifetime.current===controller){
    if(error instanceof APIError&&[401,403,404].includes(error.status)){setLoaded(undefined);attempt.current=null;setError('scene_access_denied');}
    else if(error instanceof APIError&&[409,410,412,422].includes(error.status)){attempt.current=null;setError('scene_reference_conflict');}
    else setError(error instanceof APIError&&error.status===503?'scene_reference_unavailable':'scene_reference_failed');
   }
  }
  finally{lock.current=false;if(!controller.signal.aborted&&lifetime.current===controller)setSubmitting(false);}
 }
 const board=loaded?.api===api&&loaded.reload===reload?loaded.board:undefined;
 return <SafeAreaView style={s.root}><View style={s.header}><LocalePicker/><Pressable accessibilityRole="button" disabled={busy||submitting||!!attempt.current} onPress={()=>setReload(value=>value+1)} style={s.button}><Text style={s.buttonText}>{t('scene_reload')}</Text></Pressable>{error&&attempt.current?<Pressable accessibilityRole="button" disabled={submitting} onPress={()=>void submitReference()} style={s.button}><Text style={s.buttonText}>{t('scene_retry_analysis')}</Text></Pressable>:null}{error?<Text accessibilityRole="alert" style={s.error}>{t(error)}</Text>:null}</View>{busy?<ActivityIndicator/>:null}{board?<SceneBoardView board={board} busy={submitting||!!attempt.current} onReference={(groupId,versionId)=>void submitReference(groupId,versionId)}/>:null}</SafeAreaView>;
}
