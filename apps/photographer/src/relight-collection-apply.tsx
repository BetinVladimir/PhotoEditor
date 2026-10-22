import {useLocale} from './locale-context';
import {Button,Text} from 'react-native-paper';
import AsyncStorage from '@react-native-async-storage/async-storage';
import {useEffect,useRef,useState} from 'react';
import {View} from 'react-native';
import {APIError} from '@photoeditor/api-client';
import {CollectionVersionStore} from './collection-version-store';
import {freezeCollectionVersion,type CollectionVersionSelection} from './collection-version';
import type {RelightCollectionTarget} from './relight-collection-targets';
import type {OwnerAPI} from './owner-api';
import type {Review} from './catalog-types';
import {useReadResource} from './use-read-resource';
import {ReadResourceState} from './read-resource-state';
import {catalogStyle as s} from './catalog-style';

export function RelightCollectionApply({api,origin,tenant,review,onOpen,onAccessDenied}:{api:OwnerAPI;origin:string;tenant:string;review:Review;onOpen:(selection:CollectionVersionSelection)=>void;onAccessDenied?:()=>void}){
 const __uiMessage=useLocale().message;

 const __uiT=useLocale().t;

 const resource=useReadResource(JSON.stringify([origin,tenant,review.id,review.decision_hash]),api,async signal=>{
  const actor=(await api.me(signal)).id;signal.throwIfAborted();
  const store=new CollectionVersionStore(AsyncStorage,origin,actor,tenant,review.id,review.decision_hash!),pending=await store.read();signal.throwIfAborted();
  const page=pending?{items:[] as RelightCollectionTarget[],next_cursor:null}:await api.relightCollectionTargets(review,null,signal);signal.throwIfAborted();
  if((await api.me(signal)).id!==actor)throw new Error('Collection actor changed');signal.throwIfAborted();
  return{actor,store,pending,targets:page.items,cursor:page.next_cursor};
 });
 const data=resource.data,[busy,setBusy]=useState(false),[error,setError]=useState(''),flight=useRef<AbortController|null>(null),authored=useRef<CollectionVersionSelection|null>(null),cursors=useRef(new Set<string>());
 useEffect(()=>{flight.current?.abort();flight.current=null;authored.current=null;cursors.current.clear();setBusy(false);setError('');return()=>{flight.current?.abort();flight.current=null;};},[api,origin,tenant,review.id,review.decision_hash,resource.phase]);
 function cancel(){flight.current?.abort();flight.current=null;setBusy(false);setError('Collection response is unresolved. Recover the saved choice before choosing again.');}
 async function execute(action:'select'|'discard'|'more',target?:RelightCollectionTarget){
  if(!data||flight.current||!resource.current())return;
  const controller=new AbortController();flight.current=controller;setBusy(true);setError('');
  const current=()=>!controller.signal.aborted&&flight.current===controller&&resource.current();
  const check=async()=>{controller.signal.throwIfAborted();if(!current())throw new DOMException('Collection scope changed','AbortError');const actor=(await api.me(controller.signal)).id;controller.signal.throwIfAborted();if(!current()||actor!==data.actor)throw new Error('Collection actor changed');};
  const timer=setTimeout(()=>{if(current())cancel();},15000);
  try{
   await check();
   if(action==='more'){
    const cursor=data.cursor;if(!cursor)return;
    const page=await api.relightCollectionTargets(review,cursor,controller.signal);await check();
    if(cursors.current.has(cursor)||page.next_cursor&&(page.next_cursor===cursor||cursors.current.has(page.next_cursor))||new Set(page.items.map(item=>item.item)).size!==page.items.length||page.items.some(item=>data.targets.some(old=>old.item===item.item)))throw new Error('Collection page invalid');
    cursors.current.add(cursor);resource.commit({...data,targets:[...data.targets,...page.items],cursor:page.next_cursor});return;
   }
   const scoped={reconcileCollectionVersion:async(value:CollectionVersionSelection,signal?:AbortSignal)=>{await check();const result=await api.reconcileCollectionVersion(value,signal);await check();return result;},replaceCollectionVersion:async(value:CollectionVersionSelection,signal?:AbortSignal)=>{await check();const result=await api.replaceCollectionVersion(value,signal);await check();return result;}};
   if(action==='discard'){
    await data.store.discardConflicted(scoped,controller.signal);await check();authored.current=null;
    const page=await api.relightCollectionTargets(review,null,controller.signal);await check();cursors.current.clear();resource.commit({...data,pending:null,targets:page.items,cursor:page.next_cursor});return;
   }
   let pending=await data.store.read();await check();pending=pending??authored.current;
   if(!pending){
    if(!target)throw new Error('No saved collection choice');const applied=await api.applyRelightSelected(review,controller.signal);await check();
    if(applied.asset_id!==target.asset)throw new Error('Collection asset changed');
    pending=freezeCollectionVersion({collection:target.collection,item:target.item,asset:target.asset,revision:target.revision,expectedVersion:target.expectedVersion,version:applied.asset_version_id});
   }
   authored.current=pending;resource.commit({...data,pending});
   await data.store.write(pending);await check();
   const result=await data.store.resume(scoped,controller.signal);await check();authored.current=null;resource.commit({...data,pending:null});onOpen(result);
  }catch(cause){if(current()){
   if(cause instanceof APIError&&[401,403,404,410].includes(cause.status)||cause instanceof Error&&cause.message==='Collection actor changed'){resource.cancel();onAccessDenied?.();return;}
   setError(action==='discard'?'The saved choice has no confirmed conflict. Recover it before choosing again.':action==='more'?'Unable to load more collections. Retry the same page.':'Collection changed or the response was lost. Recover the saved choice; conflicting edits will not be overwritten.');
  }}finally{clearTimeout(timer);if(flight.current===controller){flight.current=null;if(resource.current())setBusy(false);}}
 }
 if(!data)return <ReadResourceState {...resource}/>;
 return <View><Text style={s.name}>{__uiT('ui_43228b6b5302')}</Text>{data.pending?<Button accessibilityRole="button" disabled={busy} onPress={()=>void execute('select')}    mode="contained" contentStyle={{minHeight:48}}>{__uiT('ui_a9feecd6a27f')}</Button>:data.targets.map(target=><Button key={target.item} accessibilityRole="button" disabled={busy} onPress={()=>void execute('select',target)}    mode="contained" contentStyle={{minHeight:48}}>{__uiT('ui_792c9a3f9a85')}{' '}{target.name}</Button>)}{data.pending&&error?<Button accessibilityRole="button" disabled={busy} onPress={()=>void execute('discard')} mode="outlined">{__uiT('ui_3defc4bb098d')}</Button>:null}{!data.pending&&data.cursor?<Button accessibilityRole="button" disabled={busy} onPress={()=>void execute('more')} mode="outlined">{__uiT('ui_3ba05a778e55')}</Button>:null}{!busy&&!data.pending&&data.targets.length===0&&!error?<Text>{__uiT('ui_9e36875911e9')}</Text>:null}{busy?<Button accessibilityRole="button" onPress={cancel}    mode="contained" contentStyle={{minHeight:48}}>{__uiT('ui_1a0b725f942f')}</Button>:null}{error?<Text accessibilityRole="alert" style={s.error}>{__uiMessage(error,true)}</Text>:null}</View>;
}
