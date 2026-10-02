import {APIError} from '@photoeditor/api-client';
import type {RunStatus} from './catalog-types';
const terminal=new Set(['completed','completed_with_errors','failed','cancelled']);
type Event={cursor:string;type:string;payload:unknown};
export interface RunWatchReader {
 load(signal:AbortSignal):Promise<RunStatus>;
 stream(cursor:string|undefined,receive:(event:Event)=>void,signal:AbortSignal):Promise<void>;
}
/** Events invalidate catalog state; their payload never supplies authoritative run state. */
export function watchRun(reader:RunWatchReader,id:string,signal:AbortSignal,receive:(run:RunStatus)=>void,failed:(error:unknown)=>void){
 let denied=false,timer:ReturnType<typeof setInterval>|undefined;
 const inactive=()=>signal.aborted||denied;
 const report=(error:unknown)=>{if(inactive())return;if(error instanceof APIError&&[401,403,404].includes(error.status)){denied=true;if(timer!==undefined)clearInterval(timer);}failed(error);};
 let latest:RunStatus|undefined,cursor:string|undefined,flight:Promise<void>|undefined,pending=false,reconnectDelay=2000,lastPoll=Date.now();
 const accept=(value:RunStatus)=>{
  if(inactive()||value.id!==id||!/^[1-9][0-9]{0,15}$/.test(value.revision)||BigInt(value.revision)>9007199254740991n)return;
  if(latest&&BigInt(value.revision)<BigInt(latest.revision))return;
  latest=value;lastPoll=Date.now();receive(value);
 };
 const refresh=():Promise<void>=>{
  if(inactive())return Promise.resolve();
  if(flight){pending=true;return flight;}
  flight=(async()=>{do{pending=false;try{accept(await reader.load(signal));}catch(error){report(error);}}while(pending&&!inactive());})().finally(()=>{flight=undefined;});
  return flight;
 };
 const pause=(delay:number)=>new Promise<void>(resolve=>{
  const done=()=>{clearTimeout(timer);signal.removeEventListener('abort',done);resolve();};
  const timer=setTimeout(done,delay);signal.addEventListener('abort',done,{once:true});
  if(signal.aborted)done();
 });
 timer=setInterval(()=>{const now=Date.now(),delay=latest&&terminal.has(latest.status)?10000:2000;if(now-lastPoll>=delay){lastPoll=now;void refresh();}},2000);
 const abort=()=>{if(timer!==undefined)clearInterval(timer);};
 signal.addEventListener('abort',abort,{once:true});
 if(signal.aborted)abort();
 void refresh();
 void(async()=>{
  while(!inactive()){
   try{await reader.stream(cursor,event=>{
    if(inactive())return;
    cursor=event.cursor;reconnectDelay=2000;
    if(event.payload&&typeof event.payload==='object'&&'run_id' in event.payload&&event.payload.run_id===id)void refresh();
   },signal);}catch(error){if(error instanceof APIError&&error.status===410)cursor=undefined;else if(error instanceof APIError&&[401,403,404].includes(error.status))report(error);/* Polling remains available when streaming disconnects. */}
   if(!inactive()){void refresh();await pause(reconnectDelay);reconnectDelay=Math.min(30000,reconnectDelay*2);}
  }
  signal.removeEventListener('abort',abort);
 })();
 return {refresh,accept};
}
