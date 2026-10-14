import {perspectivePreviewBase64} from './perspective-preview';
import type {MultiIntent} from './multi-intent';
import {useEffect,useState} from 'react';
import {Image,Platform,Pressable,Text,View} from 'react-native';
import type {OwnerAPI} from './owner-api';
import type {MultiProposal} from './multi-proposal';
import {downloadMultiPreview} from './multi-preview-download';
import {useLocale} from './locale-context';

export function multiPreviewDisplayScope(proposal:MultiProposal,intent:MultiIntent):string{
 return JSON.stringify([proposal,intent]);
}

export function MultiPreviewView({api,proposal,intent}:{api:Pick<OwnerAPI,'multiPreview'>;proposal:MultiProposal;intent:MultiIntent}){
 const {t}=useLocale();
 const scope=multiPreviewDisplayScope(proposal,intent);
 const [variant,setVariant]=useState<'preview1600'|'preview2560'>('preview1600'),[refresh,setRefresh]=useState(0);
 const [display,setDisplay]=useState<{api:Pick<OwnerAPI,'multiPreview'>;scope:string;uri:string;width:number;height:number}|null>(null),[failed,setFailed]=useState(false);
 useEffect(()=>{
  const controller=new AbortController();let disposed=false,uri='',expiry:ReturnType<typeof setTimeout>|undefined;
  const active=()=>!disposed&&!controller.signal.aborted;
  const release=()=>{if(Platform.OS==='web'&&uri)URL.revokeObjectURL(uri);uri='';};
  setDisplay(null);setFailed(false);
  void(async()=>{try{
   const ref=await api.multiPreview(structuredClone(proposal),structuredClone(intent),variant,controller.signal);
   const bytes=await downloadMultiPreview(ref,controller.signal);if(!active())return;
   if(Platform.OS==='web'){
    if(typeof createImageBitmap!=='function')throw new Error('MULTI_PREVIEW_DECODER_UNAVAILABLE');
    const blob=new Blob([new Uint8Array(bytes)],{type:ref.media_type}),image=await createImageBitmap(blob);
    try{if(!active())return;if(image.width!==ref.width||image.height!==ref.height)throw new Error('MULTI_PREVIEW_DIMENSIONS');}finally{image.close();}
    uri=URL.createObjectURL(blob);
   }else{
    uri=`data:${ref.media_type};base64,${perspectivePreviewBase64(bytes)}`;
    const size=await new Promise<{width:number;height:number}>((resolve,reject)=>Image.getSize(uri,(width,height)=>resolve({width,height}),reject));
    if(size.width!==ref.width||size.height!==ref.height)throw new Error('MULTI_PREVIEW_DIMENSIONS');
   }
   if(!active()){release();return;}
   const remaining=Date.parse(ref.expires_at)-Date.now();if(remaining<=0)throw new Error('MULTI_PREVIEW_EXPIRED');
   setDisplay({api,scope:scope+':'+variant,uri,width:ref.width,height:ref.height});
   expiry=setTimeout(()=>{release();setDisplay(null);setFailed(true);},remaining);
  }catch{release();if(active())setFailed(true);}})();
  return()=>{disposed=true;controller.abort();if(expiry!==undefined)clearTimeout(expiry);release();};
 },[api,scope,proposal,variant,refresh]);
 const visible=display?.scope===scope+':'+variant&&display.api===api?display:null;
 return <View>{visible?<Image accessibilityLabel={t('multi_review_title')} source={{uri:visible.uri}} resizeMode="contain" style={{width:'100%',height:Math.min(480,480*visible.height/visible.width),backgroundColor:'#777'}} onError={()=>{setDisplay(null);setFailed(true);}}/>:<Text>{t(failed?'multi_preview_error':'multi_preview_loading')}</Text>}<Pressable accessibilityRole="button" onPress={()=>setRefresh(n=>n+1)}><Text>{t('multi_preview_refresh')}</Text></Pressable><Pressable accessibilityRole="button" onPress={()=>setVariant(v=>v==='preview1600'?'preview2560':'preview1600')}><Text>{t('multi_preview_variant').replace('{n}',variant==='preview1600'?'2560':'1600')}</Text></Pressable></View>;
}
