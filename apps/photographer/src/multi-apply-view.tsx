import {canonicalJSON} from '@photoeditor/api-client';
import AsyncStorage from '@react-native-async-storage/async-storage';
import {MultiApplyStore} from './multi-apply-store';
import {MultiApplyAttempt,type MultiApplyRecord} from './multi-apply-attempt';
import {useEffect,useMemo,useRef,useState} from 'react';
import {Pressable,Text,View} from 'react-native';
import type {OwnerAPI} from './owner-api';
import type {MultiQuality} from './multi-quality';
import type {MultiIntent} from './multi-intent';
import type {MultiApplied} from './multi-apply';
import {useLocale} from './locale-context';
type Props={api:OwnerAPI;review:MultiQuality;intent:MultiIntent;origin:string;selection?:MultiApplyRecord['selection'];disabled?:boolean;onApplied:(value:MultiApplied)=>void};
export function MultiApplyView(props:Props){
 const scope=canonicalJSON([props.origin,props.review,props.intent,props.selection??null]);
 const key=useMemo(()=>crypto.randomUUID(),[props.api,scope]);
 return <Scope key={key} {...props}/>;
}
function Scope({api,review,intent,origin,selection=null,disabled,onApplied}:Props){
 const {t}=useLocale();const [geometry,setGeometry]=useState(false),[warnings,setWarnings]=useState(false),[pending,setPending]=useState(false),[failed,setFailed]=useState(false);
 const life=useRef<AbortController|null>(null),lock=useRef(false),saved=useRef<MultiApplyAttempt|null>(null);
 const [restored,setRestored]=useState(false);
 useEffect(()=>{const controller=new AbortController();life.current=controller;return()=>{controller.abort();life.current=null;};},[]);
 const allowed=!disabled&&!pending&&geometry&&review.quality.status!=='fail'&&(review.quality.status!=='review'||warnings);
 async function apply(){const controller=life.current;if(!allowed||lock.current||!controller||controller.signal.aborted)return;lock.current=true;setPending(true);setFailed(false);
  try{
   const user=await api.me(controller.signal);if(controller.signal.aborted)return;
   const store=new MultiApplyStore(AsyncStorage,origin,user.id,review.proposal.tenant_id,review.proposal.project_id,review.proposal.producer_task_id);
   let attempt=saved.current;
   if(!attempt){const record=await store.read();if(controller.signal.aborted)return;
    if(record){
     if(canonicalJSON(record.intent)!==canonicalJSON(intent)||canonicalJSON(record.review)!==canonicalJSON(review)||canonicalJSON(record.selection)!==canonicalJSON(selection)||record.warnings!==warnings)throw new Error('MULTI_APPLY_RESTORED_SCOPE_MISMATCH');
     attempt=MultiApplyAttempt.restore(record,store);setRestored(true);
    }
    else attempt=await MultiApplyAttempt.create(review,intent,warnings,selection,store);
    saved.current=attempt;
   }
   const frozen=attempt.snapshot;
   if(canonicalJSON(frozen.intent)!==canonicalJSON(intent)||canonicalJSON(frozen.review)!==canonicalJSON(review)||canonicalJSON(frozen.selection)!==canonicalJSON(selection)||frozen.warnings!==warnings)throw new Error('MULTI_APPLY_RESTORED_SCOPE_MISMATCH');
   const result=await attempt.resume(api,controller.signal);if(!controller.signal.aborted)onApplied(result);
  }catch{if(!controller.signal.aborted)setFailed(true);}finally{lock.current=false;if(!controller.signal.aborted)setPending(false);}
 }
 return <View>
  <Pressable accessibilityRole="checkbox" accessibilityState={{checked:geometry,disabled:pending}} disabled={pending} onPress={()=>setGeometry(v=>!v)}><Text>{geometry?'☑':'☐'} {t('perspective_apply_geometry')}</Text></Pressable>
  {review.quality.status==='review'?<Pressable accessibilityRole="checkbox" accessibilityState={{checked:warnings,disabled:pending}} disabled={pending} onPress={()=>setWarnings(v=>!v)}><Text>{warnings?'☑':'☐'} {t('perspective_apply_warnings')}</Text></Pressable>:null}
  <Pressable accessibilityRole="button" accessibilityState={{disabled:!allowed,busy:pending}} disabled={!allowed} onPress={()=>void apply()}><Text>{t(pending?'perspective_apply_pending':'perspective_apply_submit')}</Text></Pressable>
  {restored?<Text>{t('perspective_apply_restored')}</Text>:null}
  {failed?<Text>{t('perspective_apply_failed')}</Text>:null}
 </View>;
}
