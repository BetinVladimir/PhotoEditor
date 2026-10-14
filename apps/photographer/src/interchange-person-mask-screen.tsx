import {useEffect,useMemo,useRef,useState} from 'react';
import {ActivityIndicator,Image,Platform,Pressable,SafeAreaView,ScrollView,Text,View} from 'react-native';
import type {ApiClient} from '@photoeditor/api-client';
import {OwnerAPI} from './owner-api';
import type {ImportPersonMaskCandidate,ImportPersonMaskEntry} from './interchange-person-mask-candidates';
import type {ImportPersonMaskStatus} from './interchange-person-mask-status';
import {readPersonMaskPreviewPNG} from './interchange-person-mask-preview';
import {perspectivePreviewBase64} from './perspective-preview';
import {useLocale} from './locale-context';
import {catalogStyle as s} from './catalog-style';

type Props={client:ApiClient;tenant:string;project:string;workflow:string};
type Visible={entry:string;mask:string;override:string;photo:string;overlay:string;dispose():void};
type MaskErrorKey='interchange_mask_entries_error'|'interchange_mask_candidates_error'|'interchange_mask_preview_error'|'interchange_mask_decision_error'|'';
const identity=(candidate:ImportPersonMaskCandidate)=>`${candidate.decision.override_artifact_id}:${candidate.decision.mask_artifact_id}`;

export function InterchangePersonMaskScreen(props:Props){
  const key=JSON.stringify([props.client.origin,props.tenant,props.project,props.workflow]);
  return <ScopedScreen key={key} {...props}/>;
}

function ScopedScreen({client,tenant,project,workflow}:Props){
  const {t}=useLocale(),api=useMemo(()=>new OwnerAPI(client,tenant),[client,tenant]);
  const [entries,setEntries]=useState<ImportPersonMaskEntry[]|null>(null),[entryID,setEntryID]=useState('');
  const [candidates,setCandidates]=useState<ImportPersonMaskCandidate[]|null>(null),[selected,setSelected]=useState('');
  const [visible,setVisible]=useState<Visible|null>(null),[error,setError]=useState<MaskErrorKey>(''),[busy,setBusy]=useState(false),[decision,setDecision]=useState<'granted'|'revoked'|''>('');
  const [photoLoaded,setPhotoLoaded]=useState(false),[maskLoaded,setMaskLoaded]=useState(false);
  const [statuses,setStatuses]=useState<ImportPersonMaskStatus[]|null>(null);
  const mutation=useRef<AbortController|null>(null);
  const statusRead=useRef<AbortController|null>(null);
  const displayedScope=useRef('');
  const entry=entries?.find(item=>item.entry_id===entryID);
  const candidate=candidates?.find(item=>identity(item)===selected);

  useEffect(()=>{
    const controller=new AbortController();setEntries(null);setEntryID('');setError('');
    void api.importPersonMaskEntries(project,workflow,controller.signal).then(page=>{if(!controller.signal.aborted)setEntries(page.entries);}).catch(()=>{if(!controller.signal.aborted){setEntries([]);setError('interchange_mask_entries_error');}});
    return()=>controller.abort();
  },[api,project,workflow]);
  useEffect(()=>{
    const controller=new AbortController();statusRead.current=controller;setStatuses(null);
    void api.importPersonMaskStatuses(project,workflow,controller.signal).then(items=>{if(!controller.signal.aborted)setStatuses(items);}).catch(()=>{if(!controller.signal.aborted)setError('interchange_mask_decision_error');});
    return()=>{controller.abort();if(statusRead.current===controller)statusRead.current=null;};
  },[api,project,workflow]);
  useEffect(()=>{
    const controller=new AbortController();setCandidates(null);setSelected('');setDecision('');setVisible(null);
    if(entryID)void api.importPersonMaskCandidates(project,workflow,entryID,controller.signal).then(page=>{
      if(!controller.signal.aborted&&entries?.some(item=>item.entry_id===entryID&&page.candidates.every(candidate=>candidate.decision.target_version_id===item.target_version_id)))setCandidates(page.candidates);
    }).catch(()=>{if(!controller.signal.aborted)setError('interchange_mask_candidates_error');});
    return()=>controller.abort();
  },[api,project,workflow,entryID,entries]);
  useEffect(()=>{
    const controller=new AbortController();let objectURL='';let expiry:ReturnType<typeof setTimeout>|undefined;
    displayedScope.current='';setVisible(null);setDecision('');setPhotoLoaded(false);setMaskLoaded(false);
    if(entry&&candidate)void(async()=>{
      try{
        const [png,media]=await Promise.all([
          api.importPersonMaskPreview(project,workflow,entry.entry_id,candidate.decision.override_artifact_id,candidate.decision.mask_artifact_id,controller.signal),
          api.mediaLinks(entry.asset_id,entry.target_version_id,['preview1600','thumb640'],controller.signal),
        ]);
        controller.signal.throwIfAborted();
        const mask=JSON.parse(candidate.decision.mask_jcs!) as {canvas:{width:number;height:number}};
        const dimensions=readPersonMaskPreviewPNG(png,candidate.decision.mask_jcs!);
        const link=media.items.find(item=>item.asset_version_id===entry.target_version_id&&['preview1600','thumb640'].includes(item.variant)&&['image/jpeg','image/png','image/webp'].includes(item.media_type)&&item.width>0&&item.height>0&&Date.parse(item.expires_at)>Date.now()+5000&&Math.abs(item.width/item.height-mask.canvas.width/mask.canvas.height)<0.01);
        if(!link)throw new Error('PHOTO_PREVIEW_UNAVAILABLE');
        const url=new URL(link.url);
        if(!['https:','http:'].includes(url.protocol)||!url.hostname||url.username||url.password||url.hash||url.protocol==='http:'&&!['localhost','[::1]'].includes(url.hostname)&&!/^127(?:\.[0-9]{1,3}){3}$/.test(url.hostname))throw new Error('PHOTO_PREVIEW_UNAVAILABLE');
        let overlay='';
        if(Platform.OS==='web'){
          const blob=new Blob([new Uint8Array(png)],{type:'image/png'});
          if(typeof createImageBitmap!=='function')throw new Error('MASK_PREVIEW_DECODER_UNAVAILABLE');
          const bitmap=await createImageBitmap(blob);
          try{if(bitmap.width!==dimensions.width||bitmap.height!==dimensions.height)throw new Error('MASK_PREVIEW_DIMENSIONS');}finally{bitmap.close();}
          controller.signal.throwIfAborted();objectURL=URL.createObjectURL(blob);overlay=objectURL;
        }else{
          overlay=`data:image/png;base64,${perspectivePreviewBase64(png)}`;
          const imageSize=await new Promise<{width:number;height:number}>((resolve,reject)=>Image.getSize(overlay,(width,height)=>resolve({width,height}),reject));
          if(imageSize.width!==dimensions.width||imageSize.height!==dimensions.height)throw new Error('MASK_PREVIEW_DIMENSIONS');
        }
        controller.signal.throwIfAborted();
        displayedScope.current=`${entry.entry_id}:${identity(candidate)}`;
        setVisible({entry:entry.entry_id,mask:candidate.decision.mask_artifact_id,override:candidate.decision.override_artifact_id,photo:url.toString(),overlay,dispose:()=>{if(objectURL){URL.revokeObjectURL(objectURL);objectURL='';}}});
        expiry=setTimeout(()=>{displayedScope.current='';setVisible(value=>{value?.dispose();return null;});},Math.min(2147483647,Date.parse(link.expires_at)-Date.now()));
      }catch{if(!controller.signal.aborted)setError('interchange_mask_preview_error');}
    })();
    return()=>{controller.abort();if(expiry)clearTimeout(expiry);if(objectURL)URL.revokeObjectURL(objectURL);};
  },[api,project,workflow,entry,candidate]);
  useEffect(()=>()=>mutation.current?.abort(),[]);

  async function decide(action:'grant'|'revoke'){
    if(!candidate||!entry||!visible||!photoLoaded||!maskLoaded||visible.entry!==entry.entry_id||visible.mask!==candidate.decision.mask_artifact_id||visible.override!==candidate.decision.override_artifact_id||mutation.current)return;
    const controller=new AbortController();mutation.current=controller;setBusy(true);setError('');
    try{
      const source=candidate.decision;
      const request=action==='grant'?source:{action:'revoke' as const,entry_id:source.entry_id,target_version_id:source.target_version_id,override_artifact_id:source.override_artifact_id,mask_artifact_id:source.mask_artifact_id};
      const receipt=await api.decideImportPersonMask(project,workflow,request,controller.signal);
      if(!controller.signal.aborted){
        statusRead.current?.abort();
        setDecision(receipt.status);
        setStatuses(items=>{
          const match=(item:ImportPersonMaskStatus)=>item.entry_id===source.entry_id&&item.override_artifact_id===source.override_artifact_id&&item.mask_artifact_id===source.mask_artifact_id&&item.target_version_id===source.target_version_id;
          const next:ImportPersonMaskStatus={entry_id:source.entry_id,override_artifact_id:source.override_artifact_id,mask_artifact_id:source.mask_artifact_id,target_version_id:source.target_version_id,status:receipt.status,epoch:receipt.epoch};
          return items?.some(match)?items.map(item=>match(item)?next:item):[...(items??[]),next];
        });
      }
    }catch{if(!controller.signal.aborted)setError('interchange_mask_decision_error');}
    finally{if(mutation.current===controller)mutation.current=null;setBusy(false);}
  }

  async function revokeSaved(item:ImportPersonMaskStatus){
    if(item.status!=='granted'||mutation.current)return;
    const controller=new AbortController();mutation.current=controller;setBusy(true);setError('');
    try{
      const receipt=await api.decideImportPersonMask(project,workflow,{action:'revoke',entry_id:item.entry_id,target_version_id:item.target_version_id,override_artifact_id:item.override_artifact_id,mask_artifact_id:item.mask_artifact_id},controller.signal);
      if(!controller.signal.aborted){statusRead.current?.abort();setStatuses(items=>items?.map(row=>row===item?{...row,status:'revoked',epoch:receipt.epoch}:row)??null);}
    }catch{if(!controller.signal.aborted)setError('interchange_mask_decision_error');}
    finally{if(mutation.current===controller)mutation.current=null;setBusy(false);}
  }

  const current=visible&&entry&&candidate&&visible.entry===entry.entry_id&&visible.mask===candidate.decision.mask_artifact_id&&visible.override===candidate.decision.override_artifact_id?visible:null;
  return <SafeAreaView style={s.root}><ScrollView contentContainerStyle={{padding:20,gap:12}}>
    <Text style={s.title}>{t('interchange_mask_title')}</Text>
    {statuses?.map(item=><View key={`${item.entry_id}:${item.override_artifact_id}:${item.mask_artifact_id}:${item.target_version_id}`}><Text style={s.subtitle}>{item.entry_id.slice(0,8)} · {item.mask_artifact_id.slice(0,8)} · {t(item.status==='granted'?'interchange_mask_granted':'interchange_mask_revoked')}</Text>{item.status==='granted'?<Pressable style={s.button} accessibilityRole="button" disabled={busy} onPress={()=>void revokeSaved(item)}><Text style={s.buttonText}>{t('interchange_mask_revoke')}</Text></Pressable>:null}</View>)}
    {!entries?<ActivityIndicator accessibilityLabel={t('interchange_mask_loading')}/>:entries.length===0?<Text style={s.subtitle}>{t('interchange_mask_entries_empty')}</Text>:entries.map(item=><Pressable key={item.entry_id} style={s.button} accessibilityRole="button" onPress={()=>{mutation.current?.abort();setEntryID(item.entry_id);setError('');}}><Text style={s.buttonText}>{item.external_id}{item.entry_id===entryID?' ✓':''}</Text></Pressable>)}
    {entry&&!candidates?<ActivityIndicator accessibilityLabel={t('interchange_mask_loading')}/>:null}
    {entry&&candidates?.length===0?<Text style={s.subtitle}>{t('interchange_mask_candidates_empty')}</Text>:null}
    {candidates?.map(item=><Pressable key={identity(item)} style={s.button} accessibilityRole="button" onPress={()=>{mutation.current?.abort();setSelected(identity(item));setError('');}}><Text style={s.buttonText}>{t('interchange_mask_candidate')} · {item.decision.mask_artifact_id.slice(0,8)}{selected===identity(item)?' ✓':''}</Text></Pressable>)}
    {candidate&&!current?<ActivityIndicator accessibilityLabel={t('interchange_mask_loading')}/>:null}
    {current?<View><Text style={s.subtitle}>{t('interchange_mask_preview')}</Text><View style={{width:'100%',aspectRatio:1,position:'relative',backgroundColor:'#202124'}}><Image accessibilityLabel={t('interchange_mask_photo')} source={{uri:current.photo}} resizeMode="contain" style={{width:'100%',height:'100%'}} onLoad={()=>{if(displayedScope.current===`${entryID}:${selected}`)setPhotoLoaded(true);}} onError={()=>{displayedScope.current='';setVisible(value=>{value?.dispose();return null;});}}/><Image accessibilityLabel={t('interchange_mask_overlay')} source={{uri:current.overlay}} resizeMode="contain" style={{position:'absolute',left:0,right:0,top:0,bottom:0,width:'100%',height:'100%'}} onLoad={()=>{if(displayedScope.current===`${entryID}:${selected}`)setMaskLoaded(true);}} onError={()=>{displayedScope.current='';setVisible(value=>{value?.dispose();return null;});}}/></View>
      <Text style={s.subtitle}>{t('interchange_mask_explain')}</Text>
      {decision?<Text style={s.subtitle} accessibilityLiveRegion="polite">{t(decision==='granted'?'interchange_mask_granted':'interchange_mask_revoked')}</Text>:null}
      <Pressable style={s.button} accessibilityRole="button" disabled={busy||!photoLoaded||!maskLoaded} onPress={()=>void decide('grant')}><Text style={s.buttonText}>{t('interchange_mask_grant')}</Text></Pressable>
      <Pressable style={s.button} accessibilityRole="button" disabled={busy||!photoLoaded||!maskLoaded} onPress={()=>void decide('revoke')}><Text style={s.buttonText}>{t('interchange_mask_revoke')}</Text></Pressable>
    </View>:null}
    {error?<Text style={s.error} accessibilityRole="alert">{t(error)}</Text>:null}
  </ScrollView></SafeAreaView>;
}
