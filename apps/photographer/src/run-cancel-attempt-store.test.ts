import {describe,it,expect} from 'vitest';
import {RunCancelAttemptStore,clearRunCancelAttempts} from './run-cancel-attempt-store';

const id=(n:number)=>`00000000-0000-0000-0000-${n.toString().padStart(12,'0')}`;
function storage(){const items=new Map<string,string>();return {getItem:async(key:string)=>items.get(key)??null,setItem:async(key:string,value:string)=>{items.set(key,value);},removeItem:async(key:string)=>{items.delete(key);},getAllKeys:async()=>[...items.keys()]};}

describe('run cancel attempt journal',()=>{
 it('reuses the key after response loss and clears only the acknowledged key',async()=>{
  const data=storage(),one=new RunCancelAttemptStore(data,'cancel-origin',id(1),id(2),id(3));
  const first=await one.begin();
  const mounted=new RunCancelAttemptStore(data,'cancel-origin',id(1),id(2),id(3));
  expect(await mounted.begin()).toEqual(first);
  await expect(mounted.acknowledge(id(9))).rejects.toThrow('RUN_CANCEL_ATTEMPT_CONFLICT');
  expect(await mounted.read()).toEqual(first);
  await mounted.acknowledge(first.key);
  expect(await one.read()).toBeNull();
  const rejected=await one.begin();
  await one.discard(rejected.key);
  expect(await mounted.read()).toBeNull();
 });
 it('isolates user scope and clears all attempts at logout',async()=>{
  const data=storage(),one=new RunCancelAttemptStore(data,'cancel-logout',id(1),id(2),id(3));
  await one.begin();
  expect(await new RunCancelAttemptStore(data,'cancel-logout',id(4),id(2),id(3)).read()).toBeNull();
  await clearRunCancelAttempts(data,'cancel-logout');
  await expect(one.read()).rejects.toThrow('RUN_CANCEL_ATTEMPT_EXPIRED');
  expect(await new RunCancelAttemptStore(data,'cancel-logout',id(1),id(2),id(3)).read()).toBeNull();
 });
});
