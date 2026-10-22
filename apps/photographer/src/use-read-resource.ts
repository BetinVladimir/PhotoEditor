import {useEffect,useRef,useState} from 'react';
import {APIError} from '@photoeditor/api-client';
import {useReviewForeground} from './use-review-foreground';
export type ReadPhase='loading'|'ready'|'error'|'cancelled'|'paused';
/** Only reads are retried here. Mutations retain their own exact journal/CAS. */
export function useReadResource<T>(scope:string,connection:unknown,read:(signal:AbortSignal)=>Promise<T>,options:{foreground?:boolean}={}){
 const observed=useReviewForeground(),foreground=options.foreground===false?{active:true,generation:0}:observed,reader=useRef(read);reader.current=read;
 const [attempt,setAttempt]=useState(0),[state,setState]=useState<{scope:string;connection:unknown;generation:number;phase:ReadPhase;data?:T}>({scope,connection,generation:foreground.generation,phase:'loading'});
 const flight=useRef<AbortController|null>(null),ownership=useRef({scope,connection,generation:foreground.generation,active:foreground.active});
 ownership.current={scope,connection,generation:foreground.generation,active:foreground.active};
 const alive=useRef(true);useEffect(()=>{alive.current=true;return()=>{alive.current=false;};},[]);
 // An explicit refresh/cancel also revokes mutation callbacks retained by the
 // previous render, even when tenant, connection and foreground are unchanged.
 const epoch=useRef(0),version=epoch.current;
 const generation=foreground.generation;
 const current=()=>alive.current&&epoch.current===version&&ownership.current.scope===scope&&ownership.current.connection===connection&&ownership.current.generation===generation&&ownership.current.active;
 useEffect(()=>{
  const controller=new AbortController();flight.current=controller;
  const own=()=>flight.current===controller&&!controller.signal.aborted&&current();
  if(!foreground.active){setState({scope,connection,generation,phase:'paused'});return()=>{controller.abort();if(flight.current===controller)flight.current=null;};}
  setState({scope,connection,generation,phase:'loading'});
  const timeout=setTimeout(()=>{if(own()){epoch.current++;setState({scope,connection,generation,phase:'error'});controller.abort();}},8000);
  void(async()=>{try{
   const value=await reader.current(controller.signal);
   if(own())setState({scope,connection,generation,phase:'ready',data:value});
  }catch(error){if(own()){
   // Never retain data across a denied/expired scope. Other errors also leave
   // the read retry explicit, avoiding accidental mutation replay.
   if(error instanceof APIError&&[401,403,404,410].includes(error.status))controller.abort();
   epoch.current++;setState({scope,connection,generation,phase:'error'});
  }}finally{clearTimeout(timeout);}})();
  return()=>{clearTimeout(timeout);controller.abort();if(flight.current===controller)flight.current=null;};
 },[scope,connection,generation,foreground.active,attempt]);
 const owned=state.scope===scope&&state.connection===connection&&state.generation===generation&&foreground.active;
 return {
  data:owned?state.data:undefined,phase:(foreground.active?(owned?state.phase:'loading'):'paused') as ReadPhase,
  retry:()=>{if(!current())return;epoch.current++;flight.current?.abort();setState({scope,connection,generation,phase:'loading'});setAttempt(value=>value+1);},
  cancel:()=>{if(!current())return;epoch.current++;flight.current?.abort();setState({scope,connection,generation,phase:'cancelled'});},
  current,
  commit:(data:T)=>{if(current())setState({scope,connection,generation,phase:'ready',data});},
 };
}
