import {useLocale} from './locale-context';
import type en from '../../../locales/en/common.json';
import {useEffect,useMemo,useRef,useState} from 'react';
import {AppState,Image,Pressable,Text,View,ActivityIndicator} from 'react-native';
import type {ApiClient,RunOutput} from '@photoeditor/api-client';

type Props={client:Pick<ApiClient,'runOutputs'>;tenant:string;run:string};
export function SemanticPreview(props:Props){const scope=useMemo(()=>crypto.randomUUID(),[props.client,props.tenant,props.run]);return <SemanticPreviewScope key={scope} {...props}/>;}
function SemanticPreviewScope({client,tenant,run}:Props){
 const {t}=useLocale();
 const[image,setImage]=useState<RunOutput|null>(null),[busy,setBusy]=useState(false),[error,setError]=useState<keyof typeof en|''>('');
 const active=useRef(AppState.currentState===null||AppState.currentState==='active'),[foreground,setForeground]=useState(active.current);
 const flight=useRef<AbortController|null>(null),expiry=useRef<ReturnType<typeof setTimeout>|null>(null);
 const clear=()=>{setImage(null);if(expiry.current!==null)clearTimeout(expiry.current);expiry.current=null};
 async function load(){
  if(flight.current||!active.current)return;
  clear();setError('');setBusy(true);const controller=new AbortController();flight.current=controller;
  try{
   const page=await client.runOutputs(tenant,run,controller.signal);
   if(controller.signal.aborted)return;
   const output=page.items.find(item=>item.role==='derivative'&&['image/jpeg','image/webp','image/png'].includes(item.media_type)&&Date.parse(item.expires_at)>Date.now());
   if(!output){setError('semantic_preview_unavailable');return}
   setImage(output);expiry.current=setTimeout(()=>{setImage(null);setError('semantic_preview_expired');expiry.current=null},Math.min(2147483647,Date.parse(output.expires_at)-Date.now()));
  }catch{if(!controller.signal.aborted){clear();setError('semantic_preview_failed')}}
  finally{if(flight.current===controller){flight.current=null;if(!controller.signal.aborted)setBusy(false)}}
 }
 useEffect(()=>{
  clear();setBusy(false);setError('');
  const listener=AppState.addEventListener('change',state=>{
   active.current=state==='active';setForeground(active.current);
   if(!active.current){flight.current?.abort();flight.current=null;clear();setBusy(false);setError('');}
  });
  return()=>{listener.remove();flight.current?.abort();flight.current=null;if(expiry.current!==null)clearTimeout(expiry.current);expiry.current=null};
 },[client,tenant,run]);
 return <View><Pressable accessibilityRole="button" disabled={busy||!foreground} onPress={()=>void load()}><Text>{t(image?'semantic_preview_reload':'semantic_preview_view')}</Text></Pressable>{busy?<ActivityIndicator accessibilityLabel={t('semantic_preview_loading')}/>:null}{error?<Text accessibilityRole="alert">{t(error)}</Text>:null}{image?<Image accessibilityLabel={t('semantic_preview_image')} source={{uri:image.url}} resizeMode="contain" style={{width:280,height:220}} onError={()=>{clear();setError('semantic_preview_display_failed')}}/>:null}</View>;
}
