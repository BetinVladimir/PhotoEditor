import {describe,it,expect} from 'vitest';
import {RunRetryAttemptStore,clearRunRetryAttempts} from './run-retry-attempt-store';

const id=(n:number)=>`00000000-0000-0000-0000-${n.toString().padStart(12,'0')}`;
function storage(){const items=new Map<string,string>();return {getItem:async(key:string)=>items.get(key)??null,setItem:async(key:string,value:string)=>{items.set(key,value);},removeItem:async(key:string)=>{items.delete(key);},getAllKeys:async()=>[...items.keys()]};}

describe('run retry attempt journal',()=>{
 it('reuses the request after response loss and clears only after acknowledgement',async()=>{
  const data=storage(),one=new RunRetryAttemptStore(data,'retry-origin',id(1),id(2),id(3));
  const first=await one.begin('9007199254740993');
  const mounted=new RunRetryAttemptStore(data,'retry-origin',id(1),id(2),id(3));
  expect(await mounted.begin('9007199254740994')).toEqual(first);
  await expect(mounted.acknowledge(id(9))).rejects.toThrow('RUN_RETRY_ATTEMPT_CONFLICT');
  expect(await mounted.read()).toEqual(first);
  await mounted.acknowledge(first.key);
  expect(await one.read()).toBeNull();
 });
 it('isolates users and invalidates attempts on logout',async()=>{
  const data=storage(),first=new RunRetryAttemptStore(data,'logout-origin',id(1),id(2),id(3));
  await first.begin('1');
  expect(await new RunRetryAttemptStore(data,'logout-origin',id(4),id(2),id(3)).read()).toBeNull();
  await clearRunRetryAttempts(data,'logout-origin');
  await expect(first.read()).rejects.toThrow('RUN_RETRY_ATTEMPT_EXPIRED');
  expect(await new RunRetryAttemptStore(data,'logout-origin',id(1),id(2),id(3)).read()).toBeNull();
 });
});
