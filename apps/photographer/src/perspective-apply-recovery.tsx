import {Button,Text} from 'react-native-paper';
import {APIError} from '@photoeditor/api-client';
import {perspectiveApplyTransport} from './perspective-apply-transport';
import {useEffect,useMemo,useRef,useState} from 'react';
import {AppState,View} from 'react-native';
import AsyncStorage from '@react-native-async-storage/async-storage';
import type {OwnerAPI} from './owner-api';
import type {PerspectiveApplied} from './perspective-apply';
import {PerspectiveApplyAttempt} from './perspective-apply-attempt';
import {PerspectiveApplyStore} from './perspective-apply-store';
import {useLocale} from './locale-context';
export function PerspectiveApplyRecovery(props:Parameters<typeof PerspectiveApplyRecoveryScope>[0]){
 const scope=useMemo(()=>crypto.randomUUID(),[props.api,props.origin,props.tenant,props.project,props.version,props.producer,props.collection]);
 const [generation,setGeneration]=useState(0),[foreground,setForeground]=useState((AppState?.currentState??'active')==='active');
 useEffect(()=>{if(!AppState?.addEventListener)return;const listener=AppState.addEventListener('change',state=>{setForeground(state==='active');if(state==='active')setGeneration(value=>value+1);});return()=>listener.remove();},[]);
 return foreground?<PerspectiveApplyRecoveryScope key={`${scope}:${generation}`} {...props}/>:null;
}
function PerspectiveApplyRecoveryScope({api,origin,tenant,project,version,producer,collection,disabled,onApplied,onAccessDenied}:{api:OwnerAPI;origin:string;tenant:string;project:string;version:string;producer?:string;collection?:string;disabled?:boolean;onAccessDenied?:()=>void;onApplied:(value:PerspectiveApplied)=>void}){
 const disabledNow=useRef(disabled);disabledNow.current=disabled;
 const {t}=useLocale();
 const scope=JSON.stringify([origin,tenant,project,version,producer??null,collection??null]);
 const [entry,setEntry]=useState<{api:OwnerAPI;scope:string;user:string;store:PerspectiveApplyStore;attempt:PerspectiveApplyAttempt}|null>(null),[failed,setFailed]=useState(false),[denied,setDenied]=useState(false),[busy,setBusy]=useState(false),[refresh,setRefresh]=useState(0);
 const life=useRef<AbortController|null>(null),lock=useRef(false);
 useEffect(()=>{
  const controller=new AbortController();life.current=controller;lock.current=false;setBusy(false);setEntry(null);setFailed(false);
  void(async()=>{try{
   const user=await api.me(controller.signal);if(controller.signal.aborted)return;
   const store=new PerspectiveApplyStore(AsyncStorage,origin,user.id,tenant,project,version);
   const record=await store.read();if(controller.signal.aborted)return;
   if(record&&producer&&record.review.proposal.producer_task_id!==producer)throw new Error("PERSPECTIVE_APPLY_PRODUCER_SCOPE_MISMATCH");
   if(record&&collection!==undefined&&record.selection!==null&&record.selection.collection!==collection)throw new Error("PERSPECTIVE_APPLY_COLLECTION_SCOPE_MISMATCH");
   if(record)setEntry({api,scope,user:user.id,store,attempt:PerspectiveApplyAttempt.restore(record,store)});
  }catch(error){if(!controller.signal.aborted){if(error instanceof APIError&&[401,403,404,410].includes(error.status)){setDenied(true);onAccessDenied?.();}setFailed(true);}}})();
  return()=>{controller.abort();if(life.current===controller)life.current=null;};
 },[api,origin,tenant,project,version,producer,collection,scope,refresh]);
 const current=entry?.api===api&&entry.scope===scope?entry:null;
 async function resume(){const controller=life.current;if(!current||denied||disabled||lock.current||!controller||controller.signal.aborted)return;
  lock.current=true;setBusy(true);setFailed(false);
  try{const before=await api.me(controller.signal);controller.signal.throwIfAborted();
   if(before.id!==current.user){setDenied(true);onAccessDenied?.();setEntry(null);setFailed(true);return;}
   const result=await current.attempt.resume(perspectiveApplyTransport(api,()=>life.current===controller&&!disabledNow.current),controller.signal);controller.signal.throwIfAborted();const after=await api.me(controller.signal);controller.signal.throwIfAborted();
   if(after.id!==current.user){setDenied(true);onAccessDenied?.();setEntry(null);setFailed(true);return;}onApplied(result);}catch(error){if(!controller.signal.aborted){if(error instanceof APIError&&[401,403,404,410].includes(error.status)){setDenied(true);onAccessDenied?.();setEntry(null);}setFailed(true);}}finally{if(life.current===controller){lock.current=false;if(!controller.signal.aborted)setBusy(false);}}
 }
 async function clear(){const controller=life.current;if(!current||lock.current||!controller||controller.signal.aborted)return;
  lock.current=true;setBusy(true);
  try{await current.store.clear(current.attempt.snapshot.review.quality_result_id);if(!controller.signal.aborted)setEntry(null);}catch{if(!controller.signal.aborted)setFailed(true);}finally{if(life.current===controller){lock.current=false;if(!controller.signal.aborted)setBusy(false);}}
 }
 if(!current&&!failed)return null;
 const record=current?.attempt.snapshot,completed=!!record?.applied&&(record.selection===null||record.selected);
 return <View><Text>{t('perspective_apply_restored')}</Text>
  {current?<><Text>{t(record?.applied?'perspective_apply_version_saved':'perspective_apply_decision_saved')}</Text>
   <Button accessibilityRole="button" disabled={busy||disabled} onPress={()=>void resume()} mode="outlined">{t('perspective_apply_resume')}</Button>
   {completed?<Button accessibilityRole="button" disabled={busy} onPress={()=>void clear()} mode="outlined">{t('perspective_apply_clear_completed')}</Button>:null}</>:null}
  {failed?<><Text>{t('perspective_apply_failed')}</Text><Button accessibilityRole="button" disabled={busy||denied} onPress={()=>setRefresh(n=>n+1)} mode="outlined">{t('perspective_apply_reload')}</Button></>:null}
 </View>;
}
