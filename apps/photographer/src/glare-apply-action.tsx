import {useLocale} from './locale-context';
import {Button,Text} from 'react-native-paper';
import {readDeadline} from './read-deadline';
import {useEffect,useMemo,useRef,useState} from 'react';
import {AppState,View} from 'react-native';
import type {OwnerAPI} from './owner-api';
import type {Review} from './catalog-types';
import type {GlareAppliedVersion} from './glare-applied-version';
import {APIError} from '@photoeditor/api-client';
import {catalogStyle as s} from './catalog-style';

export function GlareApplyAction({api,review,onOpen,onAccessDenied}:{api:OwnerAPI;review:Review;onOpen:(value:GlareAppliedVersion)=>void;onAccessDenied?:()=>void}){
 const __uiMessage=useLocale().message;

 const __uiT=useLocale().t;

 const [busy,setBusy]=useState(false),[error,setError]=useState(''),[denied,setDenied]=useState(false),[foreground,setForeground]=useState((AppState.currentState??'active')==='active');
 const active=useRef<AbortController|null>(null),scope=useRef(0);
 const binding=useMemo(()=>({api,id:review.id,decision:review.decision_hash}),[api,review.id,review.decision_hash]);
 const live=useRef(binding),alive=useRef(false),visible=useRef(foreground),deniedAuthority=useRef(false);live.current=binding;
 useEffect(()=>{alive.current=true;return()=>{alive.current=false;};},[]);
 useEffect(()=>{scope.current++;deniedAuthority.current=false;setBusy(false);setError('');setDenied(false);return()=>{scope.current++;active.current?.abort();active.current=null}},[api,review.id,review.decision_hash]);
 useEffect(()=>{const listener=AppState.addEventListener('change',state=>{visible.current=state==='active';scope.current++;active.current?.abort();active.current=null;setBusy(false);setError('');setForeground(state==='active');});return()=>listener.remove()},[]);
 const displayed=active.current,displayedScope=scope.current;
 function cancel(){if(!alive.current||live.current!==binding||!displayed||active.current!==displayed||scope.current!==displayedScope)return;scope.current++;displayed.abort();active.current=null;setBusy(false);setError('Saving is unresolved. Retry safely to recover the same selected version.');}
 async function apply(){
  if(!alive.current||live.current!==binding||active.current||deniedAuthority.current||!visible.current)return;
  const controller=new AbortController(),generation=scope.current;active.current=controller;setBusy(true);setError('');
  const owns=()=>alive.current&&live.current===binding&&visible.current&&scope.current===generation&&active.current===controller;
  try{
   const {before,result,after}=await readDeadline(controller.signal,async signal=>{
    const check=()=>{signal.throwIfAborted();if(!owns())throw new Error('SELECTED_RESULT_SCOPE_CHANGED');};
    check();const before=await api.me(signal);check();
    const result=await api.applyGlareSelected(review,signal);check();
    const after=await api.me(signal);check();return {before,result,after};
   },15000);if(!owns())return;
   if(before.id!==after.id){
    if(scope.current===generation){deniedAuthority.current=true;setDenied(true);setError('The account changed. Reopen the review before applying a result.');onAccessDenied?.();}
    return;
   }
   if(scope.current===generation)onOpen(result);
  }catch(failure){if(!controller.signal.aborted&&owns()){if(failure instanceof APIError&&[401,403,404,410].includes(failure.status)){deniedAuthority.current=true;setDenied(true);onAccessDenied?.();setError('The selected result is no longer accessible. Reopen the review after access is restored.')}else setError('Unable to save the selected result. Retry safely to recover the same version.');}}
  finally{if(active.current===controller){active.current=null;if(scope.current===generation)setBusy(false)}}
 }
 if(review.kind!=='portrait.glare'||review.state!=='decided'||!['accept','reject'].includes(review.decision??''))return null;
 return <View><Button accessibilityRole="button" disabled={busy||denied||!foreground} onPress={()=>void apply()}    mode="contained" contentStyle={{minHeight:48}}>{busy?__uiT('ui_23e39291d613'):review.decision==='reject'?__uiT('ui_f7fb0f14c5b6'):__uiT('ui_7117fa4a5dad')}</Button>{busy?<Button accessibilityRole="button" onPress={cancel}    mode="contained" contentStyle={{minHeight:48}}>{__uiT('ui_b4b0f02809d0')}</Button>:null}{error?<Text accessibilityRole="alert" style={s.error}>{__uiMessage(error,true)}</Text>:null}</View>;
}
