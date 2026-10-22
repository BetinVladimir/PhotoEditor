import {useEffect,useRef,useState} from 'react';
import {AppState} from 'react-native';
import type {Capabilities} from './catalog-types';

type Client={capabilities:(signal?:AbortSignal)=>Promise<Capabilities>};
export function useEditorCapabilities(client:Client,scope:string,onError:()=>void){
 const [snapshot,setSnapshot]=useState<{client:Client;scope:string;value:Capabilities}|null>(null);
 const current=useRef<Capabilities|undefined>(undefined),owner=useRef({client,scope}),error=useRef(onError);error.current=onError;
 if(owner.current.client!==client||owner.current.scope!==scope){owner.current={client,scope};current.current=undefined;}
 const value=snapshot?.client===client&&snapshot.scope===scope?snapshot.value:undefined;
 useEffect(()=>{
  let flight:AbortController|null=null,deadline:ReturnType<typeof setTimeout>|null=null;
  let live=true,sequence=0,foreground=(AppState.currentState??'active')==='active';
  function clear(){current.current=undefined;setSnapshot(null);}
  function cancel(){flight?.abort();flight=null;if(deadline!==null){clearTimeout(deadline);deadline=null;}}
  async function refresh(){
   if(!foreground||!live)return;cancel();const request=++sequence,controller=new AbortController();flight=controller;
   deadline=setTimeout(()=>{if(live&&sequence===request){sequence++;cancel();clear();error.current();}},8000);
   try{const value=await client.capabilities(controller.signal);if(live&&foreground&&sequence===request&&!controller.signal.aborted&&owner.current.client===client&&owner.current.scope===scope){current.current=value;setSnapshot({client,scope,value});}}
   catch{if(live&&foreground&&sequence===request&&!controller.signal.aborted){clear();error.current();}}
   finally{if(sequence===request)cancel();}
  }
  clear();void refresh();const timer=setInterval(()=>void refresh(),10000);
  const subscription=AppState.addEventListener('change',state=>{foreground=state==='active';sequence++;cancel();clear();if(foreground)void refresh();});
  return()=>{live=false;sequence++;cancel();clearInterval(timer);subscription.remove();if(owner.current.client===client&&owner.current.scope===scope)current.current=undefined;};
 },[client,scope]);
 return {capabilities:value,current};
}
