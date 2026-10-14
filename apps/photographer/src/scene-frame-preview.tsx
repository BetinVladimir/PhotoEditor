import {useEffect,useState} from 'react';
import {Image,Text,ActivityIndicator} from 'react-native';
import {APIError} from '@photoeditor/api-client';
import type {OwnerAPI} from './owner-api';
import type {SceneFrameBinding} from './scene-board';
import type {MediaLink} from './catalog-types';
import {useLocale} from './locale-context';
/** Mounted virtual cells alone request thumbnails. Links are never persisted. */
export function SceneFramePreview({api,frame,onAccessDenied}:{api:OwnerAPI;frame:SceneFrameBinding;onAccessDenied?:()=>void}){
 const {t}=useLocale();const [image,setImage]=useState<{link:MediaLink;api:OwnerAPI;assetId:string;versionId:string}>(),[loading,setLoading]=useState(true);
 useEffect(()=>{const controller=new AbortController();let expiry:ReturnType<typeof setTimeout>|undefined;setImage(undefined);setLoading(true);
  async function load(){try{if(!frame.assetId)return;const page=await api.mediaLinks(frame.assetId,frame.versionId,['thumb320'],controller.signal);if(controller.signal.aborted)return;
   const link=page.items.find(item=>item.variant==='thumb320'&&item.asset_version_id===frame.versionId&&['image/jpeg','image/png','image/webp'].includes(item.media_type)&&Number.isFinite(Date.parse(item.expires_at))&&Date.parse(item.expires_at)>Date.now()&&item.width>0&&item.height>0&&item.width<=320&&item.height<=320);
   if(!link)return;const url=new URL(link.url);if(!['https:','http:'].includes(url.protocol)||!url.hostname||url.username||url.password||url.hash||url.protocol==='http:'&&!['localhost','[::1]'].includes(url.hostname)&&!/^127(?:\.[0-9]{1,3}){3}$/.test(url.hostname))return;
   setImage({link:{...link},api,assetId:frame.assetId,versionId:frame.versionId});expiry=setTimeout(()=>{if(!controller.signal.aborted)setImage(undefined)},Math.min(2147483647,Date.parse(link.expires_at)-Date.now()));
  }catch(e){if(!controller.signal.aborted&&e instanceof APIError&&[401,403,404].includes(e.status))onAccessDenied?.();}finally{if(!controller.signal.aborted)setLoading(false);}}
  void load();return()=>{controller.abort();if(expiry)clearTimeout(expiry);};
 // Parent callbacks may change on a state render; only media scope restarts I/O.
 },[api,frame.assetId,frame.versionId]);
 if(loading)return <ActivityIndicator accessibilityLabel={t('scene_preview_loading')}/>;
 const current=image?.api===api&&image.assetId===frame.assetId&&image.versionId===frame.versionId?image.link:undefined;
 return current?<Image accessibilityLabel={`${t('compare_frame')} ${frame.ordinal+1}`} source={{uri:current.url}} resizeMode="contain" style={{width:160,height:120}} onError={()=>setImage(value=>value?.api===api&&value.assetId===frame.assetId&&value.versionId===frame.versionId?undefined:value)}/>:<Text>{t('scene_preview_unavailable')}</Text>;
}
