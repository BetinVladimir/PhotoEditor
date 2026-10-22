// @vitest-environment jsdom
import {afterEach,expect,it,vi} from 'vitest';
import {cleanup,fireEvent,render,screen} from '@testing-library/react';
vi.mock('react-native',async()=>({...await import('react-native-web'),
 Image:({source,onLoad,onError,accessibilityLabel}:any)=><img src={source.uri} alt={accessibilityLabel} onLoad={onLoad} onError={onError}/>,
}));
import {VerifiedReviewComparison} from './verified-review-comparison';
afterEach(cleanup);
const pair=(suffix:string)=>[{uri:'data:before'+suffix,width:20,height:10},{uri:'data:after'+suffix,width:20,height:10}];
it('requires both actual display load events and withdraws readiness on error',()=>{
 const viewed=vi.fn();render(<VerifiedReviewComparison images={pair('a')} onViewed={viewed}/>);
 expect(viewed).toHaveBeenLastCalledWith(false);
 fireEvent.load(screen.getByAltText('Review before'));
 expect(viewed).toHaveBeenLastCalledWith(false);
 fireEvent.load(screen.getByAltText('Review after'));
 expect(viewed).toHaveBeenLastCalledWith(true);
 fireEvent.error(screen.getByAltText('Review before'));
 expect(viewed).toHaveBeenLastCalledWith(false);
});
it('invalidates the displayed evidence when the pair changes',()=>{
 const viewed=vi.fn();const ui=render(<VerifiedReviewComparison images={pair('a')} onViewed={viewed}/>);
 fireEvent.load(screen.getByAltText('Review before'));fireEvent.load(screen.getByAltText('Review after'));
 expect(viewed).toHaveBeenLastCalledWith(true);
 ui.rerender(<VerifiedReviewComparison images={pair('b')} onViewed={viewed}/>);
 expect(viewed).toHaveBeenLastCalledWith(false);
 fireEvent.load(screen.getByAltText('Review after'));
 expect(viewed).toHaveBeenLastCalledWith(false);
 fireEvent.load(screen.getByAltText('Review before'));
 expect(viewed).toHaveBeenLastCalledWith(true);
});
it('does not accept a single-image display as a comparison',()=>{
 const viewed=vi.fn();render(<VerifiedReviewComparison images={pair('a').slice(0,1)} onViewed={viewed}/>);
 fireEvent.load(screen.getByAltText('Review before'));
 expect(viewed).toHaveBeenLastCalledWith(false);
});
