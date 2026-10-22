import {useLocale} from './locale-context';
import {Button,Text} from 'react-native-paper';
import {readDeadline} from './read-deadline';
import {useEffect,useMemo,useRef,useState} from 'react';
import {useReviewForeground} from './use-review-foreground';
import {View} from 'react-native';
import {APIError} from '@photoeditor/api-client';
import type {OwnerAPI} from './owner-api';
import type {LocalPortraitAutoAppliedVersion} from './local-portrait-result';
import {catalogStyle as s} from './catalog-style';

export function LocalPortraitRunResult({api,workflow,run,onOpen,onAccessDenied}:{api:OwnerAPI;workflow:string;run:string;onOpen:(value:LocalPortraitAutoAppliedVersion)=>void;onAccessDenied?:()=>void}){
 const __uiMessage=useLocale().message;

 const __uiT=useLocale().t;

 const [busy,setBusy]=useState(false),[denied,setDenied]=useState(false),[error,setError]=useState('');
 const foreground=useReviewForeground();
 const token=useMemo(()=>({}),[api,workflow,run,foreground.generation]);
 const latest=useRef({token,active:foreground.active});latest.current={token,active:foreground.active};
 const mounted=useRef(false),active=useRef<AbortController|null>(null);
 const current=()=>mounted.current&&latest.current.token===token&&latest.current.active;
 useEffect(()=>{mounted.current=true;setBusy(false);setDenied(false);setError('');return()=>{mounted.current=false;active.current?.abort();active.current=null}},[token]);
 useEffect(()=>{if(!foreground.active){active.current?.abort();active.current=null;setBusy(false);}},[foreground.active]);
 async function save(){
  if(!current()||active.current||denied)return;
  const controller=new AbortController();active.current=controller;setBusy(true);setError('');
  try{
   await readDeadline(controller.signal,async signal=>{
   const before=await api.me(signal);signal.throwIfAborted();if(!current())return;
   const selected=await api.localPortraitResult(workflow,run,signal);signal.throwIfAborted();if(!current())return;
   const result=await api.applyLocalPortraitResult(workflow,selected,signal);signal.throwIfAborted();if(!current())return;
   const after=await api.me(signal);signal.throwIfAborted();if(!current())return;
   if(before.id!==after.id){if(current()){setDenied(true);setError('The account changed. Reopen processing status.');onAccessDenied?.();}return;}
   if(current()&&active.current===controller)onOpen(result);
   },15000);
  }catch(failure){if(!controller.signal.aborted&&current()){
   if(failure instanceof APIError&&[401,403,404,410].includes(failure.status)){setDenied(true);setError('The result is no longer accessible. Reopen processing status.');onAccessDenied?.();}
   else setError('Unable to save the result. Retry safely to recover the same version.');
  }}finally{if(active.current===controller){active.current=null;if(current())setBusy(false)}}
 }
 const displayedFlight=active.current;
 return <View><Button accessibilityRole="button" disabled={!foreground.active||busy||denied} onPress={()=>void save()}    mode="contained" contentStyle={{minHeight:48}}>{busy?__uiT('ui_23e39291d613'):__uiT('ui_7117fa4a5dad')}</Button>{busy?<Button accessibilityRole="button" onPress={()=>{if(!current()||!displayedFlight||active.current!==displayedFlight)return;displayedFlight.abort();active.current=null;setBusy(false);setError('Waiting stopped. Retry safely to recover the same version.');}} mode="outlined">{__uiT('ui_b4943e6e0f04')}</Button>:null}{error?<Text accessibilityRole="alert" style={s.error}>{__uiMessage(error,true)}</Text>:null}</View>;
}
