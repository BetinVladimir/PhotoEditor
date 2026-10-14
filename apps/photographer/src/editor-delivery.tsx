import {useEffect,useMemo,useRef,useState} from 'react';
import {Pressable,Text,View} from 'react-native';
import type {OwnerAPI} from './owner-api';
import {renderInputSnapshotRequest,type RenderInputSnapshotRequest} from './render-input-snapshot';
import {useLocale} from './locale-context';
type Props={api:OwnerAPI;tenant:string;project:string;asset:string;version:string;disabled?:boolean;onPrepared:(snapshot:string)=>void};
export function EditorDelivery(props:Props){const generation=useMemo(()=>crypto.randomUUID(),[props.api]);return <Scope key={`${generation}:${props.tenant}:${props.project}:${props.asset}:${props.version}`} {...props}/>;}
function Scope(props:Props){
 const {t}=useLocale(),[busy,setBusy]=useState(false),[error,setError]=useState(false);
 const live=useRef(true),flight=useRef<AbortController|null>(null),intent=useRef<{body:RenderInputSnapshotRequest;key:string}|null>(null),done=useRef(false);
 useEffect(()=>{live.current=true;return()=>{live.current=false;flight.current?.abort();};},[props.api]);
 async function prepare(){
  if(props.disabled||flight.current||done.current)return;
  const controller=new AbortController();flight.current=controller;setBusy(true);setError(false);
  const active=()=>live.current&&!controller.signal.aborted;
  try{
   if(!intent.current){
    const base=await props.api.v2Binding(props.asset,controller.signal);if(!active())return;
    if(base.asset_version_id!==props.version)throw new Error('SOURCE_CHANGED');
    intent.current={body:renderInputSnapshotRequest({sources:[base]}),key:crypto.randomUUID()};
   }
   const snapshot=await props.api.createRenderInputSnapshot(props.project,intent.current.body,intent.current.key,controller.signal);
   if(active()){done.current=true;props.onPrepared(snapshot.id);}
  }catch{if(active())setError(true);}
  finally{if(flight.current===controller)flight.current=null;if(active())setBusy(false);}
 }
 return <View>{error?<Text accessibilityRole="alert">{t('delivery_source_failed')}</Text>:null}<Pressable accessibilityRole="button" disabled={props.disabled||busy||done.current} onPress={()=>void prepare()}><Text>{t(error?'delivery_source_retry':'delivery_create_title')}</Text></Pressable></View>;
}
