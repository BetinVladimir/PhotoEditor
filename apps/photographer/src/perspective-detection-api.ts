import type {OwnerAPI} from './owner-api';

// Check at the mutation boundary, after journal and server status awaits.
export function perspectiveDetectionAPI(api:OwnerAPI,available:()=>boolean){
 const requireAvailable=()=>{if(!available())throw new Error('OPERATION_UNAVAILABLE');};
 return {
  get:api.get.bind(api),
  perspectiveDetectedLines:api.perspectiveDetectedLines.bind(api),
  submitPerspectiveLineDetection:(...args:Parameters<OwnerAPI['submitPerspectiveLineDetection']>)=>{requireAvailable();return api.submitPerspectiveLineDetection(...args);},
  createRenderInputSnapshot:(...args:Parameters<OwnerAPI['createRenderInputSnapshot']>)=>{requireAvailable();return api.createRenderInputSnapshot(...args);},
 };
}
