import {Button,Text} from 'react-native-paper';
import {useEffect,useRef,useState} from 'react';
import {AppState,View} from 'react-native';
import {APIError} from '@photoeditor/api-client';
import type {OwnerAPI} from './owner-api';
import type {RegionJobTicket} from './region-preview';

export type RelightAnalysisWorkflow={run_id:string;workflow_id:string;revision:string;state:'queued'|'running'|'waiting_review'|'cancelling'|'cancelled'|'completed'|'failed'};
const uuid=(v:unknown)=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
const revision=(v:unknown)=>typeof v==='string'&&/^[1-9][0-9]{0,18}$/.test(v)&&BigInt(v)<=9223372036854775807n;
const terminal=(state:string)=>['completed','failed','cancelled'].includes(state);
export function readRelightAnalysisWorkflow(value:unknown,ticket:RegionJobTicket,previous:RelightAnalysisWorkflow|null):RelightAnalysisWorkflow{
 if(!value||typeof value!=='object'||Array.isArray(value))throw new Error('RELIGHT_ANALYSIS_STATUS_INVALID');
 const v=value as Record<string,unknown>;
 if(!uuid(v.run_id)||!uuid(v.workflow_id)||v.run_id!==ticket.run_id||v.workflow_id!==ticket.workflow_id||!revision(v.revision)||!['queued','running','waiting_review','cancelling','cancelled','completed','failed'].includes(v.state as string))throw new Error('RELIGHT_ANALYSIS_STATUS_INVALID');
 if(previous&&(v.run_id!==previous.run_id||v.workflow_id!==previous.workflow_id||BigInt(v.revision as string)<BigInt(previous.revision)||terminal(previous.state)&&v.state!==previous.state))throw new Error('RELIGHT_ANALYSIS_STATUS_REGRESSED');
 return {run_id:ticket.run_id,workflow_id:ticket.workflow_id,revision:v.revision as string,state:v.state as RelightAnalysisWorkflow['state']};
}

type Props={api:OwnerAPI;ticket:RegionJobTicket;labels:{queued:string;running:string;waiting_review:string;cancelling:string;cancelled:string;completed:string;failed:string;unavailable:string;refresh:string};onRefresh:()=>void};
/** Authenticated status is advisory UI state; result discovery always reloads
 * fresh candidate authority rather than trusting workflow outputs. */
export function RelightAnalysisProgress({api,ticket,labels,onRefresh}:Props){
 const[status,setStatus]=useState<RelightAnalysisWorkflow|null>(null),[failed,setFailed]=useState(false),[epoch,setEpoch]=useState(0);
 const retained=useRef<RelightAnalysisWorkflow|null>(null);
 useEffect(()=>{
  let active=true,foreground=(AppState.currentState??'active')==='active',flight:AbortController|null=null,timer:ReturnType<typeof setTimeout>|undefined;
  const stop=()=>{if(timer!==undefined){clearTimeout(timer);timer=undefined}flight?.abort();flight=null};
  async function poll(){
   if(!active||!foreground||flight)return;
   const controller=new AbortController();flight=controller;
   try{
    const wire=await api.get<unknown>(`/api/v1/tenants/{t}/v2-workflows/${encodeURIComponent(ticket.workflow_id)}`,controller.signal);
    if(!active||controller.signal.aborted)return;
    const next=readRelightAnalysisWorkflow(wire,ticket,retained.current);retained.current=next;setStatus(next);setFailed(false);
    if(!terminal(next.state))timer=setTimeout(()=>void poll(),next.state==='waiting_review'?10000:3000);
   }catch(cause){if(active&&!controller.signal.aborted){setFailed(true);if(!(cause instanceof APIError&&[401,403,404,410].includes(cause.status)))timer=setTimeout(()=>void poll(),15000);else setStatus(null);}}
   finally{if(flight===controller)flight=null}
  }
  const listener=AppState.addEventListener('change',state=>{
   const wasForeground=foreground;foreground=state==='active';
   if(!foreground)stop();
   else if(!wasForeground)void poll();
  });
  void poll();return()=>{active=false;stop();listener.remove()};
 },[api,ticket.run_id,ticket.workflow_id,epoch]);
 return <View>{status?<Text accessibilityLiveRegion="polite">{labels[status.state]}</Text>:null}
  {failed?<><Text accessibilityRole="alert">{labels.unavailable}</Text><Button accessibilityRole="button" onPress={()=>{setFailed(false);setEpoch(value=>value+1)}} mode="outlined">{labels.refresh}</Button></>:null}
  {status?.state==='completed'?<Button accessibilityRole="button" onPress={onRefresh} mode="outlined">{labels.refresh}</Button>:null}</View>;
}
