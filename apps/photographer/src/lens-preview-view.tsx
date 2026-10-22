import {Button,Text} from 'react-native-paper';
import {perspectivePreviewBase64} from './perspective-preview';
import {useEffect,useRef,useState} from 'react';
import {Image,Platform,View} from 'react-native';
import type {LensClient} from './lens-client';
import type {LensProposal} from './lens-proposal';
import {downloadPerspectivePreview} from './perspective-preview-download';
import {useLocale} from './locale-context';
import {APIError} from '@photoeditor/api-client';

export function lensPreviewDisplayScope(proposal:LensProposal):string{
 return JSON.stringify(proposal);
}

export function LensPreviewView({api,proposal,onAccessDenied,onReady}:{onReady?:(ready:boolean)=>void;onAccessDenied?:()=>void;api:LensClient;proposal:LensProposal}){
 const {t}=useLocale();
 const scope=lensPreviewDisplayScope(proposal);
 const [variant]=useState<'preview1600'>('preview1600'),[refresh,setRefresh]=useState(0);
 const [display,setDisplay]=useState<{api:LensClient;scope:string;uri:string;width:number;height:number;expiresAt:string;generation:number}|null>(null),[failed,setFailed]=useState(false);
 const rendered=useRef({api,scope});rendered.current={api,scope};
 const generation=useRef(0),current=useRef<{generation:number;api:LensClient;scope:string;uri:string}|null>(null);
 const invalidate=()=>{generation.current++;current.current=null;onReady?.(false);};
 const [denied,setDenied]=useState(false);
 useEffect(()=>{
  const token=++generation.current;current.current=null;onReady?.(false);
  const controller=new AbortController();let disposed=false,uri='',expiry:ReturnType<typeof setTimeout>|undefined;
  if(denied)return()=>controller.abort();
  const active=()=>!disposed&&!controller.signal.aborted&&generation.current===token;
  const release=()=>{if(Platform.OS==='web'&&uri)URL.revokeObjectURL(uri);uri='';};
  setDisplay(null);setFailed(false);onReady?.(false);
  void(async()=>{try{
   const ref=await api.preview(structuredClone(proposal),controller.signal);
   const bytes=await downloadPerspectivePreview(ref,controller.signal);if(!active())return;
   if(Platform.OS==='web'){
    if(typeof createImageBitmap!=='function')throw new Error('PERSPECTIVE_PREVIEW_DECODER_UNAVAILABLE');
    const blob=new Blob([new Uint8Array(bytes)],{type:ref.media_type}),image=await createImageBitmap(blob);
    try{if(!active())return;if(image.width!==ref.width||image.height!==ref.height)throw new Error('PERSPECTIVE_PREVIEW_DIMENSIONS');}finally{image.close();}
    uri=URL.createObjectURL(blob);
   }else{
    uri=`data:${ref.media_type};base64,${perspectivePreviewBase64(bytes)}`;
    const size=await new Promise<{width:number;height:number}>((resolve,reject)=>Image.getSize(uri,(width,height)=>resolve({width,height}),reject));
    if(size.width!==ref.width||size.height!==ref.height)throw new Error('PERSPECTIVE_PREVIEW_DIMENSIONS');
   }
   if(!active()){release();return;}
   const remaining=Date.parse(ref.expires_at)-Date.now();if(remaining<=0)throw new Error('PERSPECTIVE_PREVIEW_EXPIRED');
   current.current={generation:token,api,scope:scope+':'+variant,uri};setDisplay({api,scope:scope+':'+variant,uri,width:ref.width,height:ref.height,expiresAt:ref.expires_at,generation:token});
   expiry=setTimeout(()=>{if(generation.current!==token)return;invalidate();release();setDisplay(null);setFailed(true);onReady?.(false);},remaining);
  }catch(error){const wasActive=active();if(wasActive)invalidate();release();if(wasActive){if(error instanceof APIError&&[401,403,404,410].includes(error.status)){setDenied(true);setDisplay(null);onAccessDenied?.();}else setFailed(true);}}})();
  return()=>{disposed=true;if(generation.current===token)invalidate();controller.abort();if(expiry!==undefined)clearTimeout(expiry);release();};
 },[api,scope,proposal,variant,refresh,denied]);
 const visible=display?.scope===scope+':'+variant&&display.api===api?display:null;
 if(denied)return <Text accessibilityRole="alert">{t('lens_access_denied')}</Text>;
 return <View>{visible?<Image accessibilityLabel={t('lens_title')} source={{uri:visible.uri}} resizeMode="contain" style={{width:'100%',height:Math.min(480,480*visible.height/visible.width),backgroundColor:'#777'}} onLoad={()=>{const live=current.current;if(visible&&live&&rendered.current.api===visible.api&&rendered.current.scope+':'+variant===visible.scope&&live.generation===generation.current&&live.generation===visible.generation&&live.api===visible.api&&live.scope===visible.scope&&live.uri===visible.uri&&Date.parse(visible.expiresAt)>Date.now())onReady?.(true);}} onError={()=>{if(current.current?.generation!==visible.generation)return;invalidate();setDisplay(null);setFailed(true);onReady?.(false);}}/>:<Text>{t(failed?'perspective_result_unavailable':'perspective_result_pending')}</Text>}<Button accessibilityRole="button" onPress={()=>{invalidate();setDisplay(null);setRefresh(n=>n+1);}} mode="outlined">{t('perspective_preview_refresh')}</Button></View>;
}
