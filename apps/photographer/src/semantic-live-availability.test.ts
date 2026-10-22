import {expect,it} from 'vitest';
import type {Capabilities} from './catalog-types';
import {liveSemanticAvailability} from './semantic-kind-availability';
const ready:Capabilities={operations:['semantic.transfer','semantic.preview_retouch_region','semantic.adopt_shown_retouch_region'].map(operation=>({operation,resource_class:'cpu',supported_backends:['cpu'],available_backends:['cpu'],available:true,status:'ready' as const}))};

it('withdraws new semantic creation and approval through already-held callbacks',()=>{
 const snapshot:{current:Capabilities|undefined}={current:ready};
 const held=liveSemanticAvailability(snapshot);
 expect(held.kindAvailable('retouch_region')).toBe(true);
 expect(held.actionAvailable('retouch_region','apply')).toBe(true);
 snapshot.current=undefined;
 expect(held.kindAvailable('retouch_region')).toBe(false);
 expect(held.actionAvailable('retouch_region','preview')).toBe(false);
 expect(held.actionAvailable('retouch_region','apply')).toBe(false);
 snapshot.current=ready;
 expect(held.kindAvailable('retouch_region')).toBe(true);
});

it('checks preview and apply independently after live backend withdrawal',()=>{
 const snapshot={current:ready as Capabilities|undefined};
 const held=liveSemanticAvailability(snapshot);
 snapshot.current={operations:ready.operations.map(entry=>entry.operation==='semantic.adopt_shown_retouch_region'?{...entry,available:false,status:'unqualified',available_backends:[]}:entry)};
 expect(held.actionAvailable('retouch_region','preview')).toBe(true);
 expect(held.actionAvailable('retouch_region','apply')).toBe(false);
 expect(held.kindAvailable('retouch_region')).toBe(false);
});
