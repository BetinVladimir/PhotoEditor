import {readDeadline} from './read-deadline';
import type {ApiClient,SemanticChildStatus} from '@photoeditor/api-client';
type Client=Pick<ApiClient,'semanticChildStatus'>;
type State={status:SemanticChildStatus|null;failed:boolean};
type Watch={notify:(state:State)=>void;terminal:()=>()=>void};
const channels=new WeakMap<Client,Map<string,Channel>>();
const terminal=(status:SemanticChildStatus)=>['completed','completed_with_errors','failed','cancelled'].includes(status.status);
class Channel{
 readonly watches=new Set<Watch>();private state:State={status:null,failed:false};private controller=new AbortController();private timer:ReturnType<typeof setTimeout>|undefined;private started=false;private notified=new Set<()=>void>();
 constructor(private client:Client,private tenant:string,private run:string){}
 private deliver(watch:Watch){if(!this.watches.has(watch))return;watch.notify(structuredClone(this.state));}
 private finish(watch:Watch){if(!this.watches.has(watch))return;const callback=watch.terminal();if(!this.notified.has(callback)){this.notified.add(callback);callback();}}
 add(watch:Watch){this.watches.add(watch);this.deliver(watch);if(this.state.status&&terminal(this.state.status))this.finish(watch);if(!this.started){this.started=true;void this.poll();}}
 stop(){this.controller.abort();if(this.timer!==undefined)clearTimeout(this.timer);this.watches.clear();}
 retry(){
  if(!this.state.failed||this.controller.signal.aborted||!this.watches.size)return false;
  this.controller.abort();this.controller=new AbortController();
  if(this.timer!==undefined)clearTimeout(this.timer);this.timer=undefined;
  this.state={status:null,failed:false};for(const watch of [...this.watches])this.deliver(watch);
  void this.poll();return true;
 }
 private async poll(){
  const controller=this.controller;
  let current:SemanticChildStatus;
  try{current=await readDeadline(controller.signal,signal=>this.client.semanticChildStatus(this.tenant,this.run,signal));}catch{if(!controller.signal.aborted){this.state={status:null,failed:true};for(const watch of [...this.watches])this.deliver(watch);}return;}
  if(controller.signal.aborted)return;this.state={status:structuredClone(current),failed:false};
  const watches=[...this.watches];for(const watch of watches)this.deliver(watch);
  if(terminal(current)){for(const watch of watches)this.finish(watch);return;}
  if(this.watches.size)this.timer=setTimeout(()=>void this.poll(),3000);
 }
}
/** No persistent cache: last unsubscribe aborts IO/timer and removes the scoped channel. */
export function subscribeSemanticChild(client:Client,tenant:string,run:string,notify:Watch['notify'],onTerminal:Watch['terminal']){
 let entries=channels.get(client);if(!entries){entries=new Map();channels.set(client,entries);}const key=JSON.stringify([tenant,run]);let channel=entries.get(key);if(!channel){channel=new Channel(client,tenant,run);entries.set(key,channel);}const watch={notify,terminal:onTerminal};channel.add(watch);
 return()=>{channel!.watches.delete(watch);if(!channel!.watches.size){channel!.stop();if(entries!.get(key)===channel)entries!.delete(key);}};
}

/** Explicit authorized re-read after failure; shared subscribers cause one IO. */
export function retrySemanticChild(client:Client,tenant:string,run:string){
 return channels.get(client)?.get(JSON.stringify([tenant,run]))?.retry()??false;
}
