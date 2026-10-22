import {TouchableRipple,Text} from 'react-native-paper';
import {canonicalJSON,APIError} from '@photoeditor/api-client';
import {perspectiveApplyTransport} from './perspective-apply-transport';
import AsyncStorage from '@react-native-async-storage/async-storage';
import {PerspectiveApplyStore} from './perspective-apply-store';
import {PerspectiveApplyAttempt,type PerspectiveApplyRecord} from './perspective-apply-attempt';
import {useEffect,useMemo,useRef,useState} from 'react';
import {AppState,View} from 'react-native';
import type {OwnerAPI} from './owner-api';
import type {PerspectiveQuality} from './perspective-quality';
import type {PerspectiveApplied} from './perspective-apply';
import {useLocale} from './locale-context';
export function PerspectiveApplyView(props:Parameters<typeof PerspectiveApplyViewScope>[0]){
 const scope=useMemo(()=>crypto.randomUUID(),[props.api,props.origin,JSON.stringify(props.review),JSON.stringify(props.selection??null)]);
 const [generation,setGeneration]=useState(0),[foreground,setForeground]=useState((AppState?.currentState??'active')==='active');
 useEffect(()=>{if(!AppState?.addEventListener)return;const listener=AppState.addEventListener('change',state=>{setForeground(state==='active');if(state==='active')setGeneration(value=>value+1);});return()=>listener.remove();},[]);
 return foreground?<PerspectiveApplyViewScope key={`${scope}:${generation}`} {...props}/>:null;
}
function PerspectiveApplyViewScope({api,review,origin,selection=null,disabled,onApplied,onAccessDenied}:{api:OwnerAPI;review:PerspectiveQuality;origin:string;selection?:PerspectiveApplyRecord['selection'];disabled?:boolean;onAccessDenied?:()=>void;onApplied:(value:PerspectiveApplied)=>void}){
 const disabledNow=useRef(disabled);disabledNow.current=disabled;
 const {t}=useLocale();const [geometry,setGeometry]=useState(false),[warnings,setWarnings]=useState(false),[pending,setPending]=useState(false),[failed,setFailed]=useState(false);
 const life=useRef<AbortController|null>(null),lock=useRef(false),saved=useRef<PerspectiveApplyAttempt|null>(null);
 const [restored,setRestored]=useState(false),[denied,setDenied]=useState(false);
 useEffect(()=>{const controller=new AbortController();life.current=controller;return()=>{controller.abort();life.current=null;};},[]);
 const allowed=!denied&&!disabled&&!pending&&geometry&&review.quality.status!=='fail'&&(review.quality.status!=='review'||warnings)&&review.proposal.working_image.artifact_id!==review.proposal.base.source.artifact_id;
 async function apply(){const controller=life.current;if(!allowed||lock.current||!controller||controller.signal.aborted)return;lock.current=true;setPending(true);setFailed(false);
  try{
   const user=await api.me(controller.signal);if(controller.signal.aborted)return;
   const store=new PerspectiveApplyStore(AsyncStorage,origin,user.id,review.proposal.tenant_id,review.proposal.project_id,review.proposal.base.asset_version_id);
   let attempt=saved.current;
   if(!attempt){const record=await store.read();if(controller.signal.aborted)return;
    if(record){
     // A version-scoped journal may belong to an older proposal or collection.
     // Recover that immutable choice through its recovery view, never substitute
     // it for the result and geometry confirmation currently displayed.
     if(canonicalJSON(record.review)!==canonicalJSON(review)||canonicalJSON(record.selection)!==canonicalJSON(selection)){
      setRestored(true);setDenied(true);throw new Error('PERSPECTIVE_APPLY_SAVED_SCOPE_MISMATCH');
     }
     attempt=PerspectiveApplyAttempt.restore(record,store);setRestored(true);
    }
    else attempt=await PerspectiveApplyAttempt.create(review,warnings,selection,store);
    saved.current=attempt;
   }
   const result=await attempt.resume(perspectiveApplyTransport(api,()=>life.current===controller&&!disabledNow.current),controller.signal);controller.signal.throwIfAborted();
   const actor=await api.me(controller.signal);controller.signal.throwIfAborted();
   if(actor.id!==user.id){saved.current=null;setDenied(true);onAccessDenied?.();setFailed(true);return;}
   onApplied(result);
  }catch(error){if(!controller.signal.aborted){if(error instanceof APIError&&[401,403,404,410].includes(error.status)){saved.current=null;setDenied(true);onAccessDenied?.();}setFailed(true);}}finally{lock.current=false;if(!controller.signal.aborted)setPending(false);}
 }
 return <View>
  <TouchableRipple aria-checked={geometry} accessibilityRole="checkbox" accessibilityState={{checked:geometry,disabled:pending}} disabled={pending} onPress={()=>setGeometry(v=>!v)}><View style={{alignSelf:'stretch'}}><Text>{geometry?'☑':'☐'} {t('perspective_apply_geometry')}</Text></View></TouchableRipple>
  {review.quality.status==='review'?<TouchableRipple aria-checked={warnings} accessibilityRole="checkbox" accessibilityState={{checked:warnings,disabled:pending}} disabled={pending} onPress={()=>setWarnings(v=>!v)}><View style={{alignSelf:'stretch'}}><Text>{warnings?'☑':'☐'} {t('perspective_apply_warnings')}</Text></View></TouchableRipple>:null}
  <TouchableRipple accessibilityRole="button" accessibilityState={{disabled:!allowed,busy:pending}} disabled={!allowed} onPress={()=>void apply()}><View style={{alignSelf:'stretch'}}><Text>{t(pending?'perspective_apply_pending':'perspective_apply_submit')}</Text></View></TouchableRipple>
  {restored?<Text>{t('perspective_apply_restored')}</Text>:null}
  {failed?<Text>{t('perspective_apply_failed')}</Text>:null}
 </View>;
}
