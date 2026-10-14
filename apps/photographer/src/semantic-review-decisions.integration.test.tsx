// @vitest-environment jsdom
import React from 'react';
import {readFileSync} from 'node:fs';
import {render,screen,waitFor,fireEvent,cleanup} from '@testing-library/react';
import {it,expect,vi,afterEach} from 'vitest';
import {decodeSemanticTargets,decodeSemanticReviewStatus,decodeSemanticApplyResult,type SemanticApplyRequest} from '@photoeditor/api-client';
vi.mock('react-native',async()=>import('react-native-web'));
import {SemanticReviewDecisions} from './semantic-review-decisions';
const captured=process.env.PHOTO_TEST_V27_APPLY_RESPONSE_FILE;
afterEach(cleanup);
it.skipIf(!captured)('submits a decision from actual proposed review HTTP projections',async()=>{
 const fixture=JSON.parse(readFileSync(captured!,'utf8')) as {tenant:string;intent:string;request:SemanticApplyRequest;targets:unknown;status:unknown;response:unknown};
 const page=decodeSemanticTargets(fixture.targets,fixture.tenant,fixture.intent),status=decodeSemanticReviewStatus(fixture.status),result=decodeSemanticApplyResult(fixture.response);
 expect(page.items[0].data.state).toBe('proposed');
 const semanticReviewStatus=vi.fn().mockResolvedValue(status),applySemanticReview=vi.fn().mockResolvedValue(result),onSubmitted=vi.fn();
 render(<SemanticReviewDecisions client={{semanticReviewStatus,applySemanticReview}} tenant={fixture.tenant} intent={fixture.intent} items={page.items} onSubmitted={onSubmitted}/>);
 const decision=fixture.request.target_decisions[0];
 await waitFor(()=>expect(screen.getByLabelText(`${decision.decision} ${decision.target_id}`)).toBeTruthy());
 fireEvent.click(screen.getByLabelText(`${decision.decision} ${decision.target_id}`));fireEvent.click(screen.getByText('Submit decisions'));
 await waitFor(()=>expect(onSubmitted).toHaveBeenCalledOnce());
 expect(applySemanticReview.mock.calls[0].slice(0,3)).toEqual([fixture.tenant,fixture.intent,fixture.request]);
 expect(screen.queryByRole('alert')).toBeNull();
});
