import {describe,it,expect} from 'vitest';
import {SectionCreateAttemptStore} from './section-create-attempt-store';
const id='11111111-1111-4111-8111-111111111111',key='22222222-2222-4222-8222-222222222222';
function memory(){const data=new Map<string,string>();return {getItem:async(k:string)=>data.get(k)??null,setItem:async(k:string,v:string)=>{data.set(k,v);},removeItem:async(k:string)=>{data.delete(k);}};}
describe('durable section creation',()=>{
 it('recovers exact key/body and refuses revision/body replacement',async()=>{const storage=memory(),a=new SectionCreateAttemptStore(storage,'https://example.test',id,id,id);const attempt={schema_version:1 as const,key,request:{name:'Ceremony',position:0,expected_revision:3},ticket:null};await a.write(attempt);expect(await new SectionCreateAttemptStore(storage,'https://example.test',id,id,id).read()).toEqual(attempt);await expect(a.write({...attempt,request:{...attempt.request,expected_revision:4}})).rejects.toThrow();});
 it('retains accepted receipt until exact acknowledgment and fences another actor',async()=>{const storage=memory(),a=new SectionCreateAttemptStore(storage,'https://example.test',id,id,id);const attempt={schema_version:1 as const,key,request:{name:'Ceremony',position:0,expected_revision:3},ticket:{resource_id:id}};await a.write(attempt);expect(await new SectionCreateAttemptStore(storage,'https://example.test',key,id,id).read()).toBeNull();await expect(a.clearAccepted(key,key)).rejects.toThrow();expect(await a.read()).toEqual(attempt);await a.clearAccepted(key,id);expect(await a.read()).toBeNull();});
});
