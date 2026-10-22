import {Button,Text,ActivityIndicator} from 'react-native-paper';
import {useEffect,useMemo,useRef,useState} from 'react';
import {Image,View} from 'react-native';
import {APIError} from '@photoeditor/api-client';
import type {OwnerAPI} from './owner-api';
import type {SceneFrameBinding} from './scene-board';
import type {MediaLink} from './catalog-types';
import {useLocale} from './locale-context';
import {useReviewForeground} from './use-review-foreground';
import {readDeadline} from './read-deadline';
/** Mounted virtual cells alone request thumbnails. Links are never persisted. */
export function SceneFramePreview({api,frame,onAccessDenied}:{api:OwnerAPI;frame:Pick<SceneFrameBinding,'assetId'|'versionId'|'ordinal'>;onAccessDenied?:()=>void}){
 const __uiT=useLocale().t;

 const{t}=useLocale(),foreground=useReviewForeground(),[revision,setRevision]=useState(0),[image,setImage]=useState<{link:MediaLink;token:object}>(),[loading,setLoading]=useState(true);
 const token=useMemo(()=>({}),[api,frame.assetId,frame.versionId,foreground.active,foreground.generation,revision]),currentToken=useRef<object|null>(token);currentToken.current=token;
 const request=useRef<AbortController|null>(null),denied=useRef(onAccessDenied);denied.current=onAccessDenied;
 useEffect(()=>{
  const controller=new AbortController();request.current=controller;let expiry:ReturnType<typeof setTimeout>|undefined;setImage(undefined);setLoading(true);
  const current=()=>!controller.signal.aborted&&currentToken.current===token;
  if(!foreground.active){setLoading(false);return()=>{controller.abort();if(request.current===controller)request.current=null;};}
  void readDeadline(controller.signal,async signal=>{
   if(!frame.assetId)return;
   const page=await api.mediaLinks(frame.assetId,frame.versionId,['thumb320'],signal);signal.throwIfAborted();if(!current())return;
   const link=page.items.find(item=>item.variant==='thumb320'&&item.asset_version_id===frame.versionId&&['image/jpeg','image/png','image/webp'].includes(item.media_type)&&Number.isFinite(Date.parse(item.expires_at))&&Date.parse(item.expires_at)>Date.now()&&item.width>0&&item.height>0&&item.width<=320&&item.height<=320);
   if(!link)return;const url=new URL(link.url);if(!['https:','http:'].includes(url.protocol)||!url.hostname||url.username||url.password||url.hash||url.protocol==='http:'&&!['localhost','[::1]'].includes(url.hostname)&&!/^127(?:\.[0-9]{1,3}){3}$/.test(url.hostname))return;
   setImage({link:{...link},token});expiry=setTimeout(()=>{if(current())setImage(undefined);},Math.min(2147483647,Date.parse(link.expires_at)-Date.now()));
  }).catch(cause=>{if(current()&&cause instanceof APIError&&[401,403,404,410].includes(cause.status))denied.current?.();}).finally(()=>{if(current())setLoading(false);});
  return()=>{controller.abort();if(request.current===controller)request.current=null;if(currentToken.current===token)currentToken.current=null;if(expiry)clearTimeout(expiry);};
 },[api,frame.assetId,frame.versionId,foreground.active,token]);
 if(!foreground.active)return <Text>{t('read_paused')}</Text>;
 const current=image?.token===token&&Date.parse(image.link.expires_at)>Date.now()?image.link:undefined;
 return <View>{loading?<><ActivityIndicator accessibilityLabel={t('scene_preview_loading')}/><Button accessibilityRole="button" onPress={()=>{request.current?.abort();request.current=null;setImage(undefined);setLoading(false);}} mode="outlined">{t('read_cancel')}</Button></>:current?<Image accessibilityLabel={__uiT('ui_8cbaa55a52ef', {p0:(t('compare_frame')),p1:(frame.ordinal+1)})} source={{uri:current.url}} resizeMode="contain" style={{width:160,height:120}} onError={()=>{if(currentToken.current===token)setImage(value=>value?.token===token?undefined:value);}}/>:<Text>{t('scene_preview_unavailable')}</Text>}{!loading?<Button accessibilityRole="button" onPress={()=>{currentToken.current=null;setImage(undefined);setLoading(true);setRevision(value=>value+1);}} mode="outlined">{t('scene_preview_refresh')}</Button>:null}</View>;
}
