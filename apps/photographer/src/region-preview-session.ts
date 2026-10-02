import {freezeRegionRequest,regionJobTicket} from './region-preview';
import type {RegionJobTicket,RegionPreviewRequest} from './region-preview';
export type RegionPreviewScope={origin:string;user:string;tenant:string;asset:string;revision:string};
export type RegionPreviewSubmit=(asset:string,request:RegionPreviewRequest,key:string,signal:AbortSignal)=>Promise<RegionJobTicket>;
/** Abort is advisory; generation checks reject responses for an obsolete selection. */
export class RegionPreviewSession {
 private generation=0;
 private controller?:AbortController;
 private scope?:RegionPreviewScope;
 private ticket?:RegionJobTicket;
 constructor(private readonly submit:RegionPreviewSubmit){}
 bind(scope:RegionPreviewScope):void {
  if(Object.values(scope).some(value=>typeof value!=='string'||!value))throw new Error('REGION_SCOPE_INVALID');
  if(this.scope&&(['origin','user','tenant','asset','revision'] as const).every(key=>this.scope![key]===scope[key]))return;
  this.cancel();this.scope={...scope};
 }
 cancel():void {this.generation++;this.controller?.abort();this.controller=undefined;this.ticket=undefined;}
 clear():void {this.cancel();this.scope=undefined;}
 currentTicket():RegionJobTicket|undefined {return this.ticket?{...this.ticket}:undefined;}
 /** Keep polling/manifest IO under the same selection fence as job submission. */
 async loadCurrent<T>(loader:(ticket:RegionJobTicket,signal:AbortSignal)=>Promise<T>):Promise<T|undefined> {
  if(!this.ticket)throw new Error('REGION_TICKET_REQUIRED');
  const generation=this.generation,ticket={...this.ticket};
  this.controller?.abort();const controller=new AbortController();this.controller=controller;
  try {
   const result=await loader(ticket,controller.signal);
   return generation===this.generation&&!controller.signal.aborted?result:undefined;
  }catch(error){if(generation!==this.generation||controller.signal.aborted)return undefined;throw error;}
  finally{if(this.controller===controller)this.controller=undefined;}
 }
 async request(request:RegionPreviewRequest,key:string):Promise<RegionJobTicket|undefined> {
  if(!this.scope)throw new Error('REGION_SCOPE_REQUIRED');
  if(typeof key!=='string'||!key)throw new Error('REGION_IDEMPOTENCY_KEY_REQUIRED');
  const body=freezeRegionRequest(request),asset=this.scope.asset;
  this.cancel();const generation=this.generation,controller=new AbortController();this.controller=controller;
  try {
   const result=await this.submit(asset,body,key,controller.signal);
   if(generation!==this.generation||controller.signal.aborted)return undefined;
   this.ticket=regionJobTicket(result);return {...this.ticket};
  }catch(error){if(generation!==this.generation||controller.signal.aborted)return undefined;throw error;}
  finally{if(generation===this.generation)this.controller=undefined;}
 }
}
