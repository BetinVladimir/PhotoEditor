import type {PerspectiveApplyTransport} from './perspective-apply-attempt';

/** Result application is metadata work and does not require an inference worker.
 * Recheck editor authority after journal/authentication/reconciliation awaits. */
export function perspectiveApplyTransport(api:PerspectiveApplyTransport,active:()=>boolean):PerspectiveApplyTransport{
 const requireActive=(signal?:AbortSignal)=>{if(signal?.aborted||!active())throw new Error('PERSPECTIVE_APPLY_UNAVAILABLE');};
 return {
  applyPerspective:(review,geometry,warnings,signal)=>{requireActive(signal);return api.applyPerspective(review,geometry,warnings,signal);},
  replaceCollectionVersion:(selection,signal)=>{requireActive(signal);return api.replaceCollectionVersion(selection,signal);},
  reconcileCollectionVersion:api.reconcileCollectionVersion.bind(api),
 };
}
