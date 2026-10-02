import {regionJobTicket,type RegionJobTicket} from './region-preview';
import {waitRegionOutputs,type RegionRunReader} from './region-preview-outputs';
import {readRegionArtifact} from './region-preview-download';
import {verifyRegionManifest,type RegionManifestBinding,type VerifiedRegionManifest} from './region-preview-manifest';
/** Immutable request evidence must come from the authorized admitted plan. */
export async function loadRegionPreview(reader:RegionRunReader,ticket:RegionJobTicket,binding:RegionManifestBinding,signal:AbortSignal,transport:typeof fetch=fetch):Promise<{manifest:VerifiedRegionManifest;encodedBytes:Uint8Array}> {
 const expected=structuredClone(binding),job=regionJobTicket(ticket);
 const outputs=await waitRegionOutputs(reader,job,signal);
 const body=await readRegionArtifact(outputs.manifest,'manifest',signal,transport);
 const manifest=verifyRegionManifest(body,outputs.manifest,outputs.derivative,expected);
 const encodedBytes=await readRegionArtifact(outputs.derivative,'derivative',signal,transport);
 if(signal.aborted)throw new Error('REGION_PREVIEW_ABORTED');
 // Byte identity is verified here; decoding/ICC metadata checks precede UI display.
 return {manifest,encodedBytes};
}
