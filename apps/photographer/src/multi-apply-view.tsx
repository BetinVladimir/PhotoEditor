import {TouchableRipple,Text} from 'react-native-paper';
import {APIError,canonicalJSON} from '@photoeditor/api-client';
import AsyncStorage from '@react-native-async-storage/async-storage';
import {MultiApplyStore} from './multi-apply-store';
import {MultiApplyAttempt,type MultiApplyRecord} from './multi-apply-attempt';
import {useEffect,useMemo,useRef,useState} from 'react';
import {AppState,View} from 'react-native';
import type {OwnerAPI} from './owner-api';
import type {MultiQuality} from './multi-quality';
import type {MultiIntent} from './multi-intent';
import type {MultiApplied} from './multi-apply';
import {useLocale} from './locale-context';
type Props={api:OwnerAPI;review:MultiQuality;intent:MultiIntent;origin:string;selection?:MultiApplyRecord['selection'];disabled?:boolean;onAccessDenied?:()=>void;onApplied:(value:MultiApplied)=>void};
export function MultiApplyView(props:Props){
 const [foreground,setForeground]=useState((AppState.currentState??'active')==='active');
 useEffect(()=>{const listener=AppState.addEventListener('change',state=>setForeground(state==='active'));return()=>listener.remove()},[]);
 const scope=canonicalJSON([props.origin,props.review,props.intent,props.selection??null]);
 const key=useMemo(()=>crypto.randomUUID(),[props.api,scope]);
 return foreground?<Scope key={key} {...props}/>:null;
}
function Scope({api,review,intent,origin,selection=null,disabled,onApplied,onAccessDenied}:Props){
 const {t}=useLocale();const [geometry,setGeometry]=useState(false),[warnings,setWarnings]=useState(false),[pending,setPending]=useState(false),[failed,setFailed]=useState(false);
 const life=useRef<AbortController|null>(null),lock=useRef(false),saved=useRef<MultiApplyAttempt|null>(null);
 const [restored,setRestored]=useState(false),[denied,setDenied]=useState(false);
 useEffect(()=>{const controller=new AbortController();life.current=controller;return()=>{controller.abort();life.current=null;};},[]);
 const allowed=!denied&&!disabled&&!pending&&geometry&&review.quality.status!=='fail'&&(review.quality.status!=='review'||warnings);
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
   const result=await attempt.resume(api,controller.signal);controller.signal.throwIfAborted();
   const actor=await api.me(controller.signal);controller.signal.throwIfAborted();
   if(actor.id!==user.id){saved.current=null;setDenied(true);onAccessDenied?.();setFailed(true);return;}
   onApplied(result);
  }catch(error){if(!controller.signal.aborted){if(error instanceof APIError&&[401,403,404,410].includes(error.status)){saved.current=null;setDenied(true);onAccessDenied?.();}setFailed(true);}}finally{lock.current=false;if(!controller.signal.aborted)setPending(false);}
 }
 return <View>
  <TouchableRipple accessibilityRole="checkbox" aria-checked={geometry} accessibilityState={{checked:geometry,disabled:pending}} disabled={pending} onPress={()=>setGeometry(v=>!v)}><View style={{alignSelf:'stretch'}}><Text>{geometry?'☑':'☐'} {t('perspective_apply_geometry')}</Text></View></TouchableRipple>
  {review.quality.status==='review'?<TouchableRipple accessibilityRole="checkbox" aria-checked={warnings} accessibilityState={{checked:warnings,disabled:pending}} disabled={pending} onPress={()=>setWarnings(v=>!v)}><View style={{alignSelf:'stretch'}}><Text>{warnings?'☑':'☐'} {t('perspective_apply_warnings')}</Text></View></TouchableRipple>:null}
  <TouchableRipple accessibilityRole="button" accessibilityState={{disabled:!allowed,busy:pending}} disabled={!allowed} onPress={()=>void apply()}><View style={{alignSelf:'stretch'}}><Text>{t(pending?'perspective_apply_pending':'perspective_apply_submit')}</Text></View></TouchableRipple>
  {restored?<Text>{t('perspective_apply_restored')}</Text>:null}
  {failed?<Text>{t('perspective_apply_failed')}</Text>:null}
 </View>;
}
