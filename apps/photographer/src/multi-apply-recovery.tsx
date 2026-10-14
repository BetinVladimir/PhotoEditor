import {canonicalJSON} from '@photoeditor/api-client';
import type {MultiIntent,MultiOperation} from './multi-intent';
import {useEffect,useRef,useState} from 'react';
import {Pressable,Text,View} from 'react-native';
import AsyncStorage from '@react-native-async-storage/async-storage';
import type {OwnerAPI} from './owner-api';
import type {MultiApplied} from './multi-apply';
import {MultiApplyAttempt} from './multi-apply-attempt';
import {MultiApplyStore} from './multi-apply-store';
import {useLocale} from './locale-context';
export function MultiApplyRecovery({api,origin,tenant,project,producer,collection,operation,intent,disabled,onApplied}:{api:OwnerAPI;origin:string;tenant:string;project:string;producer:string;collection:string;operation:MultiOperation;intent:MultiIntent;disabled?:boolean;onApplied:(value:MultiApplied)=>void}){
 const {t}=useLocale();
 const scope=JSON.stringify([origin,tenant,project,producer,collection,operation,intent]);
 const [entry,setEntry]=useState<{api:OwnerAPI;scope:string;store:MultiApplyStore;attempt:MultiApplyAttempt}|null>(null),[failed,setFailed]=useState(false),[busy,setBusy]=useState(false),[refresh,setRefresh]=useState(0);
 const life=useRef<AbortController|null>(null),lock=useRef(false);
 useEffect(()=>{
  const controller=new AbortController();life.current=controller;lock.current=false;setBusy(false);setEntry(null);setFailed(false);
  void(async()=>{try{
   const user=await api.me(controller.signal);if(controller.signal.aborted)return;
   const store=new MultiApplyStore(AsyncStorage,origin,user.id,tenant,project,producer);
   const record=await store.read();if(controller.signal.aborted)return;
   if(record&&(record.review.proposal.operation!==operation||canonicalJSON(record.intent)!==canonicalJSON(intent)))throw new Error('MULTI_APPLY_INTENT_SCOPE_MISMATCH');
   if(record&&record.selection!==null&&record.selection.collection!==collection)throw new Error('MULTI_APPLY_COLLECTION_SCOPE_MISMATCH');
   if(record)setEntry({api,scope,store,attempt:MultiApplyAttempt.restore(record,store)});
  }catch{if(!controller.signal.aborted)setFailed(true);}})();
  return()=>{controller.abort();if(life.current===controller)life.current=null;};
 },[api,origin,tenant,project,producer,collection,scope,refresh]);
 const current=entry?.api===api&&entry.scope===scope?entry:null;
 async function resume(){const controller=life.current;if(!current||disabled||lock.current||!controller||controller.signal.aborted)return;
  lock.current=true;setBusy(true);setFailed(false);
  try{const result=await current.attempt.resume(api,controller.signal);if(!controller.signal.aborted)onApplied(result);}catch{if(!controller.signal.aborted)setFailed(true);}finally{if(life.current===controller){lock.current=false;if(!controller.signal.aborted)setBusy(false);}}
 }
 async function clear(){const controller=life.current;if(!current||lock.current||!controller||controller.signal.aborted)return;
  lock.current=true;setBusy(true);
  try{await current.store.clear(current.attempt.snapshot.review.quality_result_id);if(!controller.signal.aborted)setEntry(null);}catch{if(!controller.signal.aborted)setFailed(true);}finally{if(life.current===controller){lock.current=false;if(!controller.signal.aborted)setBusy(false);}}
 }
 if(!current&&!failed)return null;
 const record=current?.attempt.snapshot,completed=!!record?.applied&&(record.selection===null||record.selected);
 return <View><Text>{t('perspective_apply_restored')}</Text>
  {current?<><Text>{t(record?.applied?'perspective_apply_version_saved':'perspective_apply_decision_saved')}</Text>
   <Pressable accessibilityRole="button" disabled={busy||disabled} onPress={()=>void resume()}><Text>{t('perspective_apply_resume')}</Text></Pressable>
   {completed?<Pressable accessibilityRole="button" disabled={busy} onPress={()=>void clear()}><Text>{t('perspective_apply_clear_completed')}</Text></Pressable>:null}</>:null}
  {failed?<><Text>{t('perspective_apply_failed')}</Text><Pressable accessibilityRole="button" disabled={busy} onPress={()=>setRefresh(n=>n+1)}><Text>{t('perspective_apply_reload')}</Text></Pressable></>:null}
 </View>;
}
