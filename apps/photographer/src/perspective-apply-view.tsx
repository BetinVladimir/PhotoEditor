import AsyncStorage from '@react-native-async-storage/async-storage';
import {PerspectiveApplyStore} from './perspective-apply-store';
import {PerspectiveApplyAttempt,type PerspectiveApplyRecord} from './perspective-apply-attempt';
import {useEffect,useRef,useState} from 'react';
import {Pressable,Text,View} from 'react-native';
import type {OwnerAPI} from './owner-api';
import type {PerspectiveQuality} from './perspective-quality';
import type {PerspectiveApplied} from './perspective-apply';
import {useLocale} from './locale-context';
export function PerspectiveApplyView({api,review,origin,selection=null,disabled,onApplied}:{api:OwnerAPI;review:PerspectiveQuality;origin:string;selection?:PerspectiveApplyRecord['selection'];disabled?:boolean;onApplied:(value:PerspectiveApplied)=>void}){
 const {t}=useLocale();const [geometry,setGeometry]=useState(false),[warnings,setWarnings]=useState(false),[pending,setPending]=useState(false),[failed,setFailed]=useState(false);
 const life=useRef<AbortController|null>(null),lock=useRef(false),saved=useRef<PerspectiveApplyAttempt|null>(null);
 const [restored,setRestored]=useState(false);
 useEffect(()=>{const controller=new AbortController();life.current=controller;return()=>{controller.abort();life.current=null;};},[]);
 const allowed=!disabled&&!pending&&geometry&&review.quality.status!=='fail'&&(review.quality.status!=='review'||warnings)&&review.proposal.working_image.artifact_id!==review.proposal.base.source.artifact_id;
 async function apply(){const controller=life.current;if(!allowed||lock.current||!controller||controller.signal.aborted)return;lock.current=true;setPending(true);setFailed(false);
  try{
   const user=await api.me(controller.signal);if(controller.signal.aborted)return;
   const store=new PerspectiveApplyStore(AsyncStorage,origin,user.id,review.proposal.tenant_id,review.proposal.project_id,review.proposal.base.asset_version_id);
   let attempt=saved.current;
   if(!attempt){const record=await store.read();if(controller.signal.aborted)return;
    if(record){attempt=PerspectiveApplyAttempt.restore(record,store);setRestored(true);}
    else attempt=await PerspectiveApplyAttempt.create(review,warnings,selection,store);
    saved.current=attempt;
   }
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
