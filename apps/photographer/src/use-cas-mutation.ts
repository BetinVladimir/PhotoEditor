import {useEffect,useRef,useState} from 'react';
import {readDeadline} from './read-deadline';

/** An uncertain CAS write is reconciled by a fresh read, never replayed blindly. */
export function useCASMutation(scope:unknown,current:()=>boolean,reload:()=>void){
 const [busy,setBusy]=useState(false),[uncertain,setUncertain]=useState(false);
 const flight=useRef<AbortController|null>(null),flightAuthority=useRef<(()=>boolean)|null>(null),owner=useRef(scope);owner.current=scope;
 useEffect(()=>{setBusy(false);setUncertain(false);return()=>{flight.current?.abort();flight.current=null;flightAuthority.current=null;};},[scope]);
 // A read lifetime can expire without changing the API instance (background,
 // authority refresh or explicit read cancellation). Revoke the active write
 // locally; its server outcome must be reconciled by the next authorized read.
 useEffect(()=>{
  if(flight.current&&(!current()||!flightAuthority.current?.())){
   flight.current.abort();flight.current=null;flightAuthority.current=null;setBusy(false);setUncertain(true);
  }
 },[scope,current]);
 async function run<T>(write:(signal:AbortSignal)=>Promise<T>,accept:(value:T)=>void){
  if(owner.current!==scope||flight.current||!current())return;
  const controller=new AbortController();flight.current=controller;flightAuthority.current=current;setBusy(true);setUncertain(false);
  const owns=()=>owner.current===scope&&flight.current===controller&&!controller.signal.aborted;
  try{
   const value=await readDeadline(controller.signal,write,15000);
   if(owns()&&current())accept(value);
  }catch{
   if(owns()&&current()){setUncertain(true);reload();}
  }finally{if(flight.current===controller){flight.current=null;flightAuthority.current=null;setBusy(false);}}
 }
 function cancel(){
  if(owner.current!==scope||!current()||!flight.current)return;
  flight.current.abort();flight.current=null;flightAuthority.current=null;setBusy(false);setUncertain(true);
  if(owner.current===scope&&current())reload();
 }
 return {busy,uncertain,run,cancel};
}
