import {useEffect,useRef,useState} from 'react';
import {Pressable,Text,View} from 'react-native';
import AsyncStorage from '@react-native-async-storage/async-storage';
import type {OwnerAPI} from './owner-api';
import type {PerspectiveApplied} from './perspective-apply';
import {PerspectiveApplyAttempt} from './perspective-apply-attempt';
import {PerspectiveApplyStore} from './perspective-apply-store';
import {useLocale} from './locale-context';
export function PerspectiveApplyRecovery({api,origin,tenant,project,version,disabled,onApplied}:{api:OwnerAPI;origin:string;tenant:string;project:string;version:string;disabled?:boolean;onApplied:(value:PerspectiveApplied)=>void}){
 const {t}=useLocale();
 const [entry,setEntry]=useState<{store:PerspectiveApplyStore;attempt:PerspectiveApplyAttempt}|null>(null),[failed,setFailed]=useState(false),[busy,setBusy]=useState(false),[refresh,setRefresh]=useState(0);
 const life=useRef<AbortController|null>(null),lock=useRef(false);
 useEffect(()=>{
  const controller=new AbortController();life.current=controller;setEntry(null);setFailed(false);
  void(async()=>{try{
   const user=await api.me(controller.signal);if(controller.signal.aborted)return;
   const store=new PerspectiveApplyStore(AsyncStorage,origin,user.id,tenant,project,version);
   const record=await store.read();if(controller.signal.aborted)return;
   if(record)setEntry({store,attempt:PerspectiveApplyAttempt.restore(record,store)});
  }catch{if(!controller.signal.aborted)setFailed(true);}})();
  return()=>{controller.abort();if(life.current===controller)life.current=null;};
 },[api,origin,tenant,project,version,refresh]);
 async function resume(){const controller=life.current;if(!entry||disabled||lock.current||!controller||controller.signal.aborted)return;
  lock.current=true;setBusy(true);setFailed(false);
  try{const result=await entry.attempt.resume(api,controller.signal);if(!controller.signal.aborted)onApplied(result);}catch{if(!controller.signal.aborted)setFailed(true);}finally{lock.current=false;if(!controller.signal.aborted)setBusy(false);}
 }
 async function clear(){const controller=life.current;if(!entry||lock.current||!controller||controller.signal.aborted)return;
  lock.current=true;setBusy(true);
  try{await entry.store.clear(entry.attempt.snapshot.review.quality_result_id);if(!controller.signal.aborted)setEntry(null);}catch{if(!controller.signal.aborted)setFailed(true);}finally{lock.current=false;if(!controller.signal.aborted)setBusy(false);}
 }
 if(!entry&&!failed)return null;
 const record=entry?.attempt.snapshot,completed=!!record?.applied&&(record.selection===null||record.selected);
 return <View><Text>{t('perspective_apply_restored')}</Text>
  {entry?<><Text>{t(record?.applied?'perspective_apply_version_saved':'perspective_apply_decision_saved')}</Text>
   <Pressable accessibilityRole="button" disabled={busy||disabled} onPress={()=>void resume()}><Text>{t('perspective_apply_resume')}</Text></Pressable>
   {completed?<Pressable accessibilityRole="button" disabled={busy} onPress={()=>void clear()}><Text>{t('perspective_apply_clear_completed')}</Text></Pressable>:null}</>:null}
  {failed?<><Text>{t('perspective_apply_failed')}</Text><Pressable accessibilityRole="button" disabled={busy} onPress={()=>setRefresh(n=>n+1)}><Text>{t('perspective_apply_reload')}</Text></Pressable></>:null}
 </View>;
}
