import type {OwnerAPI} from './owner-api';
import type {ArtifactRef} from './catalog-types';
import {freezeRegionRequest,regionJobTicket,type RegionPreviewRequest,type RegionJobTicket} from './region-preview';
import {loadRegionPreview} from './region-preview-load';
import {prepareRegionDisplay,type RegionBrowserDecoder} from './region-preview-display';
import type {RegionLoupeLoader} from './region-loupe';

/** One immutable attempt, reusable after a lost response or failed download.
 * Admission retries use the same key; after a ticket is received, retries only
 * reread authorized descriptor/outputs. Mount scope owns cancellation/disposal.
 * Request.recipe must be an authorized FrozenRecipe, not an EditingRecipeV3.
 */
export function createRegionLoupeLoader(api:Pick<OwnerAPI,'regionPreview'|'regionDescriptor'|'run'|'runOutputs'>,asset:string,request:RegionPreviewRequest,source:ArtifactRef,key:string,options:{transport?:typeof fetch;decoder?:RegionBrowserDecoder}={}):RegionLoupeLoader{
 const frozen=freezeRegionRequest(request),pinned=structuredClone(source);
 if(!key||!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(pinned.artifact_id)||pinned.artifact_id==='00000000-0000-0000-0000-000000000000'||!/^[0-9a-f]{64}$/.test(pinned.sha256))throw new Error('REGION_LOUPE_BINDING_INVALID');
 let ticket:RegionJobTicket|undefined;
 return async signal=>{
  signal.throwIfAborted();
  if(!ticket){const admitted=await api.regionPreview(asset,structuredClone(frozen),key,signal);signal.throwIfAborted();ticket=regionJobTicket(admitted);}
  const binding=await api.regionDescriptor(asset,ticket.run_id,structuredClone(frozen),{...pinned},signal);signal.throwIfAborted();
  const output=await loadRegionPreview(api,ticket,binding,signal,options.transport);signal.throwIfAborted();
  return prepareRegionDisplay(output.manifest,output.encodedBytes,signal,options.decoder);
 };
}
