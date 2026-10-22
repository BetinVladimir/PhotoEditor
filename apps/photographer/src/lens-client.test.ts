import {describe,it,expect} from 'vitest';
import {freezeLensIntent,readLensTasks,readLensProfiles,LensClient} from './lens-client';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const ref=(n:number)=>({artifact_id:id(n),sha256:'a'.repeat(64)});
describe('full frozen lens batch',()=>{
 it('owns each exact choice and refuses a missing or duplicate target',()=>{
  const intent={schema_version:1,snapshot_id:id(1),choices:[{asset_version_id:id(2),profile:ref(4),strength:.5},{asset_version_id:id(3),profile:ref(5),strength:1}]};
  const own=freezeLensIntent(intent,[id(2),id(3)]);intent.choices[0].strength=0;expect(own.choices[0].strength).toBe(.5);
  expect(()=>freezeLensIntent({...intent,choices:intent.choices.slice(0,1)},[id(2),id(3)])).toThrow();
  expect(()=>freezeLensIntent({...intent,choices:[intent.choices[0],intent.choices[0]]},[id(2),id(3)])).toThrow();
 });
 it('refuses a result from a non-succeeded task or another run',()=>{
  const task={ordinal:0,asset_version_id:id(2),task_id:id(6),state:'succeeded',result_id:id(7),result:ref(8)};
  expect(readLensTasks({run_id:id(1),items:[task]},id(1)).items).toHaveLength(1);
  expect(()=>readLensTasks({run_id:id(1),items:[{...task,state:'running'}]},id(1))).toThrow();
  expect(()=>readLensTasks({run_id:id(1),items:[task]},id(9))).toThrow();
 });
 it('does not accept repeated or regressing catalog cursors',()=>{
  const p={profile:ref(4),camera_model:'Camera',lens_model:'Lens',focal_mm:50,aperture:2};
  expect(readLensProfiles({items:[p]}).items).toHaveLength(1);
  expect(()=>readLensProfiles({items:[p,p]})).toThrow();
  expect(()=>readLensProfiles({items:[p]},id(4))).toThrow();
  expect(()=>readLensProfiles({items:[p],next:id(4)})).toThrow();
 });
});

// Transport cancellation is independently fenced even if an adapter resolves late.
it.each(['profiles','status'] as const)('refuses late %s data after abort',async method=>{
 const controller=new AbortController();let resolve!:(value:unknown)=>void;
 const transport={request:()=>new Promise<unknown>(done=>{resolve=done;})};
 const client=new LensClient(transport as never,id(1),id(2));
 const pending=method==='profiles'?client.profiles(undefined,controller.signal):client.status(id(3),-1,controller.signal);
 controller.abort();resolve(method==='profiles'?{items:[]}:{run_id:id(3),items:[]});
 await expect(pending).rejects.toMatchObject({name:'AbortError'});
});
it('refuses an already cancelled catalog read before transport',async()=>{
 const controller=new AbortController();controller.abort();let calls=0;
 const client=new LensClient({request:()=>{calls++;return Promise.resolve({items:[]});}} as never,id(1),id(2));
 await expect(client.profiles(undefined,controller.signal)).rejects.toMatchObject({name:'AbortError'});expect(calls).toBe(0);
});
