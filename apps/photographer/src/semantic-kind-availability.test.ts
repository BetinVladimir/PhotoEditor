import {expect,it} from 'vitest';
import type {Capabilities} from './catalog-types';
import {semanticActionAvailable,semanticKindAvailable} from './semantic-kind-availability';
const operations=['semantic.transfer','semantic.preview_background_cleanup','semantic.adopt_shown_background_cleanup'];
function ready():Capabilities{return {operations:operations.map(operation=>({operation,resource_class:'gpu',supported_backends:['cpu','cuda','mps'],available_backends:['cuda'],available:true,status:'ready'}))};}
it('requires every deployed stage and fails closed while loading',()=>{
 expect(semanticKindAvailable(undefined,'background_cleanup')).toBe(false);
 expect(semanticKindAvailable(ready(),'background_cleanup')).toBe(true);
 for(const operation of operations){const value=ready();value.operations=value.operations.filter(entry=>entry.operation!==operation);expect(semanticKindAvailable(value,'background_cleanup')).toBe(false);}
});
it('does not borrow availability from another semantic kind or unknown implementation',()=>{
 expect(semanticKindAvailable(ready(),'remove_object')).toBe(false);
 expect(semanticKindAvailable(ready(),'retouch_region')).toBe(false);
});

it('review actions require their own stage without requiring an already completed transfer',()=>{
 const value=ready();value.operations=value.operations.filter(entry=>entry.operation!=='semantic.transfer');
 expect(semanticActionAvailable(value,'background_cleanup','preview')).toBe(true);
 expect(semanticActionAvailable(value,'background_cleanup','apply')).toBe(true);
 value.operations=value.operations.filter(entry=>entry.operation!=='semantic.adopt_shown_background_cleanup');
 expect(semanticActionAvailable(value,'background_cleanup','preview')).toBe(true);
 expect(semanticActionAvailable(value,'background_cleanup','apply')).toBe(false);
 expect(semanticActionAvailable(value,undefined,'apply')).toBe(false);
});

function retouchReady():Capabilities{return {operations:[
 {operation:'semantic.transfer',resource_class:'gpu',supported_backends:['cuda','mps'],available_backends:['cuda'],available:true,status:'ready'},
 ...['semantic.preview_retouch_region','semantic.adopt_shown_retouch_region'].map((operation):Capabilities['operations'][number]=>({operation,resource_class:'cpu',supported_backends:['cpu'],available_backends:['cpu'],available:true,status:'ready'})),
]};}
it('enables retouch only with its own transfer, preview and Apply stages',()=>{
 expect(semanticKindAvailable(retouchReady(),'retouch_region')).toBe(true);
 for(const operation of retouchReady().operations.map(entry=>entry.operation)){
  const value=retouchReady();value.operations=value.operations.filter(entry=>entry.operation!==operation);
  expect(semanticKindAvailable(value,'retouch_region')).toBe(false);
 }
});
it.each(['preview','apply'] as const)('retouch %s rejects absent, revoked, malformed and duplicate capability evidence',action=>{
 const own=action==='preview'?'semantic.preview_retouch_region':'semantic.adopt_shown_retouch_region';
 expect(semanticActionAvailable(undefined,'retouch_region',action)).toBe(false);
 for(const mode of ['absent','revoked','unqualified','empty_backend','duplicate']){
  const value=retouchReady(),entry=value.operations.find(item=>item.operation===own)!;
  if(mode==='absent')value.operations=value.operations.filter(item=>item!==entry);
  if(mode==='revoked')entry.available=false;
  if(mode==='unqualified')entry.status='unqualified';
  if(mode==='empty_backend')entry.available_backends=[];
  if(mode==='duplicate')value.operations.push({...entry});
  expect(semanticActionAvailable(value,'retouch_region',action)).toBe(false);
 }
});
it('retouch review gates preview and Apply independently after transfer has completed',()=>{
 const value=retouchReady();value.operations=value.operations.filter(entry=>entry.operation!=='semantic.transfer');
 expect(semanticActionAvailable(value,'retouch_region','preview')).toBe(true);
 expect(semanticActionAvailable(value,'retouch_region','apply')).toBe(true);
 value.operations=value.operations.filter(entry=>entry.operation!=='semantic.adopt_shown_retouch_region');
 expect(semanticActionAvailable(value,'retouch_region','preview')).toBe(true);
 expect(semanticActionAvailable(value,'retouch_region','apply')).toBe(false);
 expect(semanticKindAvailable(value,'retouch_region')).toBe(false);
});

function removalReady(backend:'cuda'|'mps'='cuda'):Capabilities{return {operations:[
 {operation:'semantic.transfer',resource_class:'gpu',supported_backends:['cuda','mps'],available_backends:[backend],available:true,status:'ready'},
 {operation:'semantic.preview_remove_object',resource_class:'gpu',supported_backends:['cpu','cuda','mps'],available_backends:[backend],available:true,status:'ready'},
 {operation:'semantic.adopt_shown_removal',resource_class:'cpu',supported_backends:['cpu'],available_backends:['cpu'],available:true,status:'ready'},
]};}
it.each(['cuda','mps'] as const)('removal requires its own preview and CPU copy on %s deployment',backend=>{
 expect(semanticKindAvailable(removalReady(backend),'remove_object')).toBe(true);
 for(const operation of removalReady(backend).operations.map(entry=>entry.operation)){
  const value=removalReady(backend);value.operations=value.operations.filter(entry=>entry.operation!==operation);
  expect(semanticKindAvailable(value,'remove_object')).toBe(false);
 }
});
it.each(['preview','apply'] as const)('removal %s fails closed on absent or contradictory capability evidence',action=>{
 const own=action==='preview'?'semantic.preview_remove_object':'semantic.adopt_shown_removal';
 expect(semanticActionAvailable(undefined,'remove_object',action)).toBe(false);
 for(const mode of ['absent','revoked','unqualified','empty_backend','unsupported_backend','duplicate']){
  const value=removalReady(),entry=value.operations.find(item=>item.operation===own)!;
  if(mode==='absent')value.operations=value.operations.filter(item=>item!==entry);
  if(mode==='revoked')entry.available=false;
  if(mode==='unqualified')entry.status='unqualified';
  if(mode==='empty_backend')entry.available_backends=[];
  if(mode==='unsupported_backend')entry.available_backends=['unknown'];
  if(mode==='duplicate')value.operations.push({...entry});
  expect(semanticActionAvailable(value,'remove_object',action)).toBe(false);
 }
});
it('removal review keeps preview and Apply availability independent after transfer completes',()=>{
 const value=removalReady('mps');value.operations=value.operations.filter(entry=>entry.operation!=='semantic.transfer');
 expect(semanticActionAvailable(value,'remove_object','preview')).toBe(true);
 expect(semanticActionAvailable(value,'remove_object','apply')).toBe(true);
 value.operations=value.operations.filter(entry=>entry.operation!=='semantic.preview_remove_object');
 expect(semanticActionAvailable(value,'remove_object','preview')).toBe(false);
 expect(semanticActionAvailable(value,'remove_object','apply')).toBe(true);
 expect(semanticKindAvailable(value,'remove_object')).toBe(false);
});
