// @vitest-environment jsdom
import React from 'react';
import {readFileSync} from 'node:fs';
import {render,screen,waitFor,fireEvent,cleanup} from '@testing-library/react';
import {it,expect,vi,afterEach} from 'vitest';
import {decodeSemanticTargets,decodeSemanticReviewStatus,decodeSemanticApplyResult,decodeSemanticReviewPreview,semanticPreviewQuery,type SemanticApplyRequest} from '@photoeditor/api-client';
import {clickWhenEnabled} from './test/ui-readiness';
// JSDOM cannot load RN Web's detached ImageLoader. Deliver all four native
// load callbacks independently; mounting alone never grants reviewed proof.
vi.mock('react-native',async()=>{
 const web=await import('react-native-web');
 return {...web,Image:({accessibilityLabel,source,onLoad,onError}:any)=><img aria-label={accessibilityLabel} src={source?.uri} onLoad={onLoad} onError={onError}/>};
});
import {SemanticReviewDecisions} from './semantic-review-decisions';
const captured=process.env.PHOTO_TEST_V27_APPLY_RESPONSE_FILE;
afterEach(cleanup);
it.skipIf(!captured)('requires real current preview proof before approving actual review HTTP projections',async()=>{
 const fixture=JSON.parse(readFileSync(captured!,'utf8')) as {tenant:string;intent:string;request:SemanticApplyRequest;targets:unknown;status:unknown;response:unknown;preview?:unknown};
 const page=decodeSemanticTargets(fixture.targets,fixture.tenant,fixture.intent),status=decodeSemanticReviewStatus(fixture.status),result=decodeSemanticApplyResult(fixture.response);
 expect(page.items[0].data.state).toBe('proposed');
 const decision=fixture.request.target_decisions[0],item=page.items.find(row=>row.id===decision.target_id)!;
 expect(decision.decision).toBe('approve');
 const semanticReviewStatus=vi.fn().mockResolvedValue(status),applySemanticReview=vi.fn().mockResolvedValue(result),onSubmitted=vi.fn();
 const query=semanticPreviewQuery(fixture.tenant,fixture.intent,item,status);
 const semanticReviewPreview=fixture.preview===undefined?undefined:vi.fn().mockImplementation(async(actual)=>{
  expect(actual).toEqual(query);return decodeSemanticReviewPreview(fixture.preview,actual);
 });
 render(<SemanticReviewDecisions client={{semanticReviewStatus,applySemanticReview,semanticReviewPreview}} tenant={fixture.tenant} intent={fixture.intent} items={page.items} onSubmitted={onSubmitted}/>);
 const approve=await screen.findByLabelText(`approve ${decision.target_id}`);
 await waitFor(()=>expect(approve.getAttribute('aria-disabled')).toBe('true'));
 expect(screen.queryByText('Submit decisions')).toBeNull();
 if(fixture.preview===undefined){
  expect(decision.preview).toBeUndefined();
  fireEvent.click(approve);
  expect(applySemanticReview).not.toHaveBeenCalled();expect(onSubmitted).not.toHaveBeenCalled();
  expect(screen.queryByText('Submit decisions')).toBeNull();return;
 }
 expect(decision.preview).toBeDefined();
 await clickWhenEnabled(screen.getByRole('button',{name:`View candidate before approval ${decision.target_id}`}));
 const images=await screen.findAllByRole('img');expect(images).toHaveLength(4);
 for(const image of images.slice(0,3))fireEvent.load(image);
 expect(approve.getAttribute('aria-disabled')).toBe('true');
 fireEvent.load(images[3]);
 await clickWhenEnabled(approve);
 await clickWhenEnabled(screen.getByText('Submit decisions'));
 await waitFor(()=>expect(onSubmitted).toHaveBeenCalledOnce());
 expect(semanticReviewPreview).toHaveBeenCalledOnce();
 expect(applySemanticReview.mock.calls[0].slice(0,3)).toEqual([fixture.tenant,fixture.intent,fixture.request]);
 expect(screen.queryByRole('alert')).toBeNull();
});
