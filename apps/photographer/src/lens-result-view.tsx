import {Button,Text} from 'react-native-paper';
import {useEffect,useMemo,useRef,useState} from 'react';
import {Image,View} from 'react-native';
import AsyncStorage from '@react-native-async-storage/async-storage';
import {useLocale} from './locale-context';
import {applyLensJournal} from './lens-apply-journal';
import type {OwnerAPI} from './owner-api';
import type {LensClient,LensTask} from './lens-client';
import type {LensProposal} from './lens-proposal';
import type {LensPrepared} from './lens-preparation';
import type {Collection,CollectionItem} from './catalog-types';
import {readEditorSourcePreview} from './editor-source-preview-link';
import {LensPreviewView} from './lens-preview-view';

export function LensResultView({canApply,owner,lens,tenant,project,task,base,item,occurrence,collection,journalScope,signal:parentSignal,authorize}:{canApply:boolean;occurrence?:Omit<import('./collection-version').CollectionVersionSelection,'version'>;collection:Collection;journalScope:string;owner:OwnerAPI;lens:LensClient;tenant:string;project:string;task:LensTask;base:NonNullable<LensPrepared['sources']>[number];item:CollectionItem;signal:AbortSignal;authorize:()=>Promise<void>}){
 const __uiMessage=useLocale().message;

 const {t}=useLocale();
 const controller=useMemo(()=>new AbortController(),[parentSignal,owner,lens,tenant,project,canApply,task.task_id,task.result_id,base.asset_version_id,base.source.artifact_id,base.source.sha256,base.canvas_hash]);
 const signal=controller.signal;
 useEffect(()=>{const revoke=()=>controller.abort();if(parentSignal.aborted)revoke();else parentSignal.addEventListener('abort',revoke,{once:true});return()=>{parentSignal.removeEventListener('abort',revoke);controller.abort();};},[controller,parentSignal]);
 const [proposal,setProposal]=useState<LensProposal>(),[before,setBefore]=useState<{url:string;expires_at:string;generation:number}>(),[beforeReady,setBeforeReady]=useState(false),[afterReady,setAfterReady]=useState(false),[ack,setAck]=useState(false),[applied,setApplied]=useState(''),[error,setError]=useState('');
 const applying=useRef(false),beforeGeneration=useRef(0),afterReadyCurrent=useRef(false);
 const onAfterReady=(ready:boolean)=>{afterReadyCurrent.current=ready;setAfterReady(ready);};
 useEffect(()=>{let active=true;const generation=++beforeGeneration.current;setBefore(undefined);setBeforeReady(false);void(async()=>{try{await authorize();const value=await lens.proposal(tenant,project,base,task.task_id,signal);if(value.result_id!==task.result_id||value.result.artifact_id!==task.result?.artifact_id||value.result.sha256!==task.result?.sha256)throw new Error('Selected result changed');const original=readEditorSourcePreview(await owner.mediaLinks(item.asset_id,base.asset_version_id,['preview1600'],signal),base.asset_version_id);if(active&&!signal.aborted&&generation===beforeGeneration.current){setProposal(value);setBefore({...original,generation});}}catch(e){if(active&&!signal.aborted)setError(t('lens_request_error'));}})();return()=>{active=false;beforeGeneration.current++;};},[owner,lens,tenant,project,task,base,item,signal]);
 useEffect(()=>{if(!before)return;const remaining=Date.parse(before.expires_at)-Date.now();if(remaining<=0){setBefore(undefined);setBeforeReady(false);return;}const timer=setTimeout(()=>{if(before.generation!==beforeGeneration.current)return;beforeGeneration.current++;setBefore(undefined);setBeforeReady(false);},remaining);return()=>clearTimeout(timer);},[before]);
 async function refreshBefore(){const generation=++beforeGeneration.current;setBefore(undefined);setBeforeReady(false);setError('');try{await authorize();const original=readEditorSourcePreview(await owner.mediaLinks(item.asset_id,base.asset_version_id,['preview1600'],signal),base.asset_version_id);if(!signal.aborted&&generation===beforeGeneration.current)setBefore({...original,generation});}catch{if(!signal.aborted&&generation===beforeGeneration.current)setError(t('lens_request_error'));}}
 async function apply(){if(!canApply||!occurrence||!proposal||!beforeReady||!afterReady||applying.current||!ack||!before||Date.parse(before.expires_at)<=Date.now())return;applying.current=true;try{await authorize();signal.throwIfAborted();if(before.generation!==beforeGeneration.current||Date.parse(before.expires_at)<=Date.now()||!afterReadyCurrent.current)throw new Error('Lens display changed');const value=await applyLensJournal(AsyncStorage,`photo.lens.apply.v1:${journalScope}:${task.task_id}:${proposal.result_id}`,JSON.parse(journalScope)[0],proposal,occurrence,lens,owner,signal);signal.throwIfAborted();setApplied(value.asset_version_id);}catch(e){if(!signal.aborted)setError(t('lens_request_error'));}finally{applying.current=false;}}
 return <View>{error?<Text accessibilityRole="alert">{__uiMessage(error,true)}</Text>:null}{before?<><Text>{t('lens_before')}</Text><Image key={before.url} source={{uri:before.url}} resizeMode="contain" style={{width:'100%',height:320}} onLoad={()=>{if(before.generation===beforeGeneration.current&&Date.parse(before.expires_at)>Date.now())setBeforeReady(true);}} onError={()=>{if(before.generation===beforeGeneration.current){beforeGeneration.current++;setBeforeReady(false);}}}/></>:null}{proposal?<><Button onPress={()=>void refreshBefore()} mode="outlined">{t('lens_refresh_before')}</Button><Text>{t('lens_after')}</Text><LensPreviewView api={lens} proposal={proposal} onReady={onAfterReady}/><Text>{t(proposal.requires_review?'lens_uncovered':'lens_pass')}</Text><Button onPress={()=>setAck(value=>!value)} mode="outlined">{ack?'✓ ':''}{t('lens_accept')}</Button><Button disabled={!canApply||!occurrence||!beforeReady||!afterReady||!ack||!!applied} onPress={()=>void apply()} mode="outlined">{t('lens_apply')}</Button></>:null}{applied?<Text>{t('lens_selected')} {applied}</Text>:null}</View>;
}
