import {useLocale} from './locale-context';
import {Button,Text} from 'react-native-paper';
import {useReadResource} from './use-read-resource';
import {ReadResourceState} from './read-resource-state';
import {useState} from 'react';
import {View} from 'react-native';
import {APIError} from '@photoeditor/api-client';
import type {OwnerAPI} from './owner-api';
import {MultiReviewView} from './multi-review-view';
import {useReviewForeground} from './use-review-foreground';
type Props={api:OwnerAPI;origin:string;tenant:string;run:string;onOpen:(asset:string,version:string)=>void;onAccessDenied:()=>void};
export function MultiRunReviewView(props:Props){
 const foreground=useReviewForeground();
 return foreground.active?<Scope key={JSON.stringify([props.origin,props.tenant,props.run,foreground.generation])} {...props}/>:null;
}
function Scope({api,origin,tenant,run,onOpen,onAccessDenied}:Props){
 const __uiT=useLocale().t;

 const[denied,setDenied]=useState(false);
 const resource=useReadResource(JSON.stringify([origin,tenant,run,'multi-result']),api,async signal=>{
  try{const before=await api.me(signal);signal.throwIfAborted();const value=await api.multiRunReview(run,signal);signal.throwIfAborted();const after=await api.me(signal);signal.throwIfAborted();if(before.id!==after.id){setDenied(true);onAccessDenied();throw new Error('MULTI_RESULT_ACTOR_CHANGED');}return value;
  }catch(error){if(!signal.aborted&&error instanceof APIError&&[401,403,404,410].includes(error.status)){setDenied(true);onAccessDenied();}throw error;}
 });
 if(denied)return null;
 if(resource.phase==='error')return <View><Text accessibilityRole="alert">{__uiT('ui_c44598982721')}</Text><Button accessibilityRole="button" onPress={resource.retry} mode="outlined">{__uiT('ui_af5a3c0490f5')}</Button></View>;
 if(resource.phase!=='ready')return <ReadResourceState phase={resource.phase} retry={resource.retry} cancel={resource.cancel}/>;
 const current=resource.data;
 return <View>{current?<MultiReviewView api={api} tenant={tenant} onAccessDenied={()=>{resource.cancel();onAccessDenied();}} attempt={{schema_version:1,operation:current.operation,project:current.project_id,key:`history:${current.run_id}`,body:current.intent,ticket:{run_id:current.run_id,workflow_id:current.workflow_id,state:'queued'}}} apply={{api,origin,collection:'',selection:null,onApplied:value=>{if(resource.current())onOpen(current.reference_asset_id,value.asset_version_id)}}}/>:null}</View>;
}
