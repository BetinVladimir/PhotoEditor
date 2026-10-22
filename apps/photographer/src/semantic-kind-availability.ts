import type {SemanticEditIntent} from '@photoeditor/api-client';
import type {Capabilities} from './catalog-types';
import {operationAvailable} from './operation-availability';

// Proposal creation promises a reviewable, applicable result. Check each
// deployed stage separately; client hardware does not determine availability.
export function semanticActionAvailable(capabilities:Capabilities|undefined,kind:SemanticEditIntent['kind']|undefined,action:'preview'|'apply'):boolean{
 const downstream:Partial<Record<SemanticEditIntent['kind'],readonly string[]>>={
  local_exposure:['semantic.preview_local_exposure','semantic.adopt_shown_exposure'],
  remove_object:['semantic.preview_remove_object','semantic.adopt_shown_removal'],
  background_cleanup:['semantic.preview_background_cleanup','semantic.adopt_shown_background_cleanup'],
  retouch_region:['semantic.preview_retouch_region','semantic.adopt_shown_retouch_region'],
 };
 const operations=kind?downstream[kind]:undefined;
 return !!operations&&operationAvailable(capabilities,operations[action==='preview'?0:1]);
}

export function semanticKindAvailable(capabilities:Capabilities|undefined,kind:SemanticEditIntent['kind']):boolean{
 return operationAvailable(capabilities,'semantic.transfer')&&semanticActionAvailable(capabilities,kind,'preview')&&semanticActionAvailable(capabilities,kind,'apply');
}
export type SemanticActionAvailability=(kind:SemanticEditIntent['kind']|undefined,action:'preview'|'apply')=>boolean;


/** Read at invocation, including async continuations before React rerenders. */
export function liveSemanticAvailability(snapshot:{readonly current:Capabilities|undefined}){
 return {
  kindAvailable:(kind:SemanticEditIntent['kind'])=>semanticKindAvailable(snapshot.current,kind),
  actionAvailable:(kind:SemanticEditIntent['kind']|undefined,action:'preview'|'apply')=>semanticActionAvailable(snapshot.current,kind,action),
 };
}
