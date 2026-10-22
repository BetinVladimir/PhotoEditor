import {beforeEach} from 'vitest';

// JSDOM has no Web Locks implementation. Model the exclusive, per-name queue
// used by browser journals; this is test transport, not a production fallback.
beforeEach(()=>{
 if(typeof document==='undefined'||navigator.locks)return;
 const pending=new Map<string,Promise<unknown>>();
 Object.defineProperty(navigator,'locks',{configurable:true,value:{
  request<T>(name:string,_options:unknown,action:()=>Promise<T>|T):Promise<T>{
   const result=(pending.get(name)??Promise.resolve()).catch(()=>{}).then(action);
   pending.set(name,result);
   void result.finally(()=>{if(pending.get(name)===result)pending.delete(name);}).catch(()=>{});
   return result;
  },
 }});
});
