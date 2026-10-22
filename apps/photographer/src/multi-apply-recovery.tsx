import {Button,Text} from 'react-native-paper';
import {APIError,canonicalJSON} from '@photoeditor/api-client';
import type {MultiIntent,MultiOperation} from './multi-intent';
import {useEffect,useRef,useState} from 'react';
import {AppState,View} from 'react-native';
import AsyncStorage from '@react-native-async-storage/async-storage';
import type {OwnerAPI} from './owner-api';
import type {MultiApplied} from './multi-apply';
import {MultiApplyAttempt} from './multi-apply-attempt';
import {MultiApplyStore} from './multi-apply-store';
import {useLocale} from './locale-context';
export function MultiApplyRecovery({api,origin,tenant,project,producer,collection,operation,intent,disabled,onApplied,onAccessDenied}:{api:OwnerAPI;origin:string;tenant:string;project:string;producer:string;collection:string;operation:MultiOperation;intent:MultiIntent;disabled?:boolean;onAccessDenied?:()=>void;onApplied:(value:MultiApplied)=>void}){
 const {t}=useLocale();
 const scope=JSON.stringify([origin,tenant,project,producer,collection,operation,intent]);
 const [entry,setEntry]=useState<{api:OwnerAPI;scope:string;actor:string;store:MultiApplyStore;attempt:MultiApplyAttempt}|null>(null),[failed,setFailed]=useState(false),[busy,setBusy]=useState(false),[refresh,setRefresh]=useState(0),[denied,setDenied]=useState(false),[foreground,setForeground]=useState((AppState.currentState??'active')==='active');
 const life=useRef<AbortController|null>(null),lock=useRef(false);
 useEffect(()=>{const listener=AppState.addEventListener('change',state=>{life.current?.abort();life.current=null;setEntry(null);setBusy(false);setForeground(state==='active');});return()=>listener.remove()},[]);
 useEffect(()=>{
  if(!foreground)return;
  setDenied(false);
  const controller=new AbortController();life.current=controller;lock.current=false;setBusy(false);setEntry(null);setFailed(false);
  void(async()=>{try{
   const user=await api.me(controller.signal);if(controller.signal.aborted)return;
   const store=new MultiApplyStore(AsyncStorage,origin,user.id,tenant,project,producer);
   const record=await store.read();if(controller.signal.aborted)return;
   if(record&&(record.review.proposal.operation!==operation||canonicalJSON(record.intent)!==canonicalJSON(intent)))throw new Error('MULTI_APPLY_INTENT_SCOPE_MISMATCH');
   if(record&&record.selection!==null&&record.selection.collection!==collection)throw new Error('MULTI_APPLY_COLLECTION_SCOPE_MISMATCH');
   if(record)setEntry({api,scope,actor:user.id,store,attempt:MultiApplyAttempt.restore(record,store)});
  }catch(error){if(!controller.signal.aborted){if(error instanceof APIError&&[401,403,404,410].includes(error.status)){setDenied(true);onAccessDenied?.();}setFailed(true);}}})();
  return()=>{controller.abort();if(life.current===controller)life.current=null;};
 },[api,origin,tenant,project,producer,collection,scope,refresh,foreground]);
 const current=entry?.api===api&&entry.scope===scope?entry:null;
 async function resume(){const controller=life.current;if(!current||disabled||lock.current||!controller||controller.signal.aborted)return;
  lock.current=true;setBusy(true);setFailed(false);
  try{const before=await api.me(controller.signal);controller.signal.throwIfAborted();if(before.id!==current.actor){setEntry(null);setDenied(true);onAccessDenied?.();setFailed(true);return;}const result=await current.attempt.resume(api,controller.signal);controller.signal.throwIfAborted();const after=await api.me(controller.signal);controller.signal.throwIfAborted();if(after.id!==current.actor){setEntry(null);setDenied(true);onAccessDenied?.();setFailed(true);return;}onApplied(result);}catch(error){if(!controller.signal.aborted){if(error instanceof APIError&&[401,403,404,410].includes(error.status)){setEntry(null);setDenied(true);onAccessDenied?.();}setFailed(true);}}finally{if(life.current===controller){lock.current=false;if(!controller.signal.aborted)setBusy(false);}}
 }
 async function clear(){const controller=life.current;if(!current||lock.current||!controller||controller.signal.aborted)return;
  lock.current=true;setBusy(true);
  try{await current.store.clear(current.attempt.snapshot.review.quality_result_id);if(!controller.signal.aborted)setEntry(null);}catch{if(!controller.signal.aborted)setFailed(true);}finally{if(life.current===controller){lock.current=false;if(!controller.signal.aborted)setBusy(false);}}
 }
 if(!foreground||!current&&!failed)return null;
 const record=current?.attempt.snapshot,completed=!!record?.applied&&(record.selection===null||record.selected);
 return <View><Text>{t('perspective_apply_restored')}</Text>
  {current?<><Text>{t(record?.applied?'perspective_apply_version_saved':'perspective_apply_decision_saved')}</Text>
   <Button accessibilityRole="button" disabled={busy||disabled} onPress={()=>void resume()} mode="outlined">{t('perspective_apply_resume')}</Button>
   {completed?<Button accessibilityRole="button" disabled={busy} onPress={()=>void clear()} mode="outlined">{t('perspective_apply_clear_completed')}</Button>:null}</>:null}
  {failed?<><Text>{t('perspective_apply_failed')}</Text><Button accessibilityRole="button" disabled={busy||denied} onPress={()=>setRefresh(n=>n+1)} mode="outlined">{t('perspective_apply_reload')}</Button></>:null}
 </View>;
}
