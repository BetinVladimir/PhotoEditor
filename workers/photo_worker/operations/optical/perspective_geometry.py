"""Resolve authored perspective strength into one immutable output geometry."""
import numpy as np
from photo_worker.algorithms.perspective import PerspectiveGeometry,manual_perspective,auto_perspective,checked_homography,perspective_coverage


def resolve_perspective_geometry(params,source,lines,*,max_pixels,check_active=lambda:None):
    width,height=source.canvas.width,source.canvas.height
    check_active()
    if params.strength==0:
        matrix=np.eye(3);matrix.flags.writeable=False
        if width*height>max_pixels:raise ValueError('PERSPECTIVE_PIXEL_LIMIT')
        return PerspectiveGeometry(matrix,width,height,0.)
    if params.mode=='manual':
        full=np.array([[0.,0.],[1.,0.],[1.,1.],[0.,1.]])
        authored=np.array([[point.x,point.y] for point in params.corners])
        corners=full+params.strength*(authored-full)
        return manual_perspective(corners,width,height,max_crop_fraction=params.max_crop_fraction,max_pixels=max_pixels)
    if lines is None:raise ValueError('PERSPECTIVE_LINES_INVALID')
    horizontal,vertical=lines.pixel_families(width,height)
    # Enforce caller's crop on the effective transform, not the full proposal.
    proposal=auto_perspective(horizontal,vertical,width,height,max_crop_fraction=1.,max_pixels=max_pixels,check_active=check_active)
    if params.strength==1:
        if proposal.crop_fraction>params.max_crop_fraction:raise ValueError('PERSPECTIVE_CROP_LIMIT')
        return proposal
    matrix=proposal.source_to_output.copy()
    if abs(matrix[2,2])<=1e-10:raise ValueError('PERSPECTIVE_DEGENERATE')
    matrix/=matrix[2,2]
    matrix=np.eye(3)+params.strength*(matrix-np.eye(3))
    corners=np.array([[0.,0.],[width,0.],[width,height],[0.,height]])
    matrix=checked_homography(matrix,corners)
    warped=np.column_stack((corners,np.ones(4)))@matrix.T
    warped=warped[:,:2]/warped[:,2,None]
    nearest=np.rint(warped)
    tolerance=64*np.finfo(np.float64).eps*np.maximum(1.,np.abs(warped))
    warped=np.where(np.abs(warped-nearest)<=tolerance,nearest,warped)
    minimum=np.floor(warped.min(axis=0));size=np.ceil(warped.max(axis=0))-minimum
    if not np.isfinite(size).all() or np.any(size<1) or np.any(size>max_pixels) or size[0]*size[1]>min(max_pixels,200000000):
        raise ValueError('PERSPECTIVE_PIXEL_LIMIT')
    translation=np.eye(3);translation[:2,2]=-minimum
    matrix=checked_homography(translation@matrix,corners)
    geometry=PerspectiveGeometry(matrix,int(size[0]),int(size[1]),0.)
    uncovered=perspective_coverage(geometry,width,height,check_active=check_active)
    if uncovered>params.max_crop_fraction:raise ValueError('PERSPECTIVE_CROP_LIMIT')
    return PerspectiveGeometry(matrix,geometry.width,geometry.height,uncovered)
