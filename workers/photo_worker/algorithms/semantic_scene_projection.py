"""Conservative full-source to bounded scene-grid projection; no authority."""
import numpy as np


def project_scene_source(rgb,labels,depth,selected,protected,edge,allowed,*,check_active=lambda:None):
    shape=rgb.shape[:2]
    if (type(edge) is not int or not 32<=edge<=1024 or not isinstance(allowed,frozenset) or not allowed
            or rgb.dtype!=np.float32 or rgb.shape!=(*shape,3) or min(shape)<edge
            or labels.shape!=shape or labels.dtype not in (np.dtype('uint8'),np.dtype('<u2'))
            or any(v.shape!=shape or v.dtype!=np.float32 for v in (depth,selected,protected))):
        raise ValueError('SEMANTIC_SCENE_SOURCE_ARRAY_INVALID')
    # All source contributors are visited. Positive selection/protection and any
    # forbidden label survive even if much smaller than a single proxy pixel.
    count=np.zeros(edge*edge,np.int64);luma=np.zeros(edge*edge,np.float64)
    depths=np.zeros(edge*edge,np.float64)
    mask=np.zeros(edge*edge,np.float32);guard=np.zeros(edge*edge,np.float32)
    unsafe=np.zeros(edge*edge,np.uint8)
    allowed=np.array(sorted(allowed),dtype=np.uint16)
    for top in range(0,shape[0],256):
        for left in range(0,shape[1],256):
            check_active();part=np.s_[top:top+256,left:left+256]
            color=rgb[part];lab=labels[part];dep=depth[part];sel=selected[part];pro=protected[part]
            if (not np.isfinite(color).all() or np.any(color<0) or np.any(color>1)
                    or np.any(lab>=150) or any(not np.isfinite(v).all() or np.any(v<0) or np.any(v>1) for v in (dep,sel,pro))):
                raise ValueError('SEMANTIC_SCENE_SOURCE_ARRAY_INVALID')
            y=np.minimum((np.arange(top,top+color.shape[0],dtype=np.int64)*edge)//shape[0],edge-1)
            x=np.minimum((np.arange(left,left+color.shape[1],dtype=np.int64)*edge)//shape[1],edge-1)
            index=(y[:,None]*edge+x[None,:]).reshape(-1)
            np.add.at(count,index,1)
            np.add.at(luma,index,(color[...,0]*.2126+color[...,1]*.7152+color[...,2]*.0722).reshape(-1))
            np.add.at(depths,index,dep.reshape(-1))
            np.maximum.at(mask,index,sel.reshape(-1));np.maximum.at(guard,index,pro.reshape(-1))
            np.maximum.at(unsafe,index,(~np.isin(lab,allowed)).astype(np.uint8).reshape(-1))
    if np.any(count==0):raise ValueError('SEMANTIC_SCENE_PROXY_GRID_INVALID')
    check_active()
    # Pixel-center source→proxy transform; exact dimensions are frozen by caller.
    sy=edge/shape[0];sx=edge/shape[1]
    transform=np.array([[sx,0,(sx-1)/2],[0,sy,(sy-1)/2],[0,0,1]],dtype=np.float64)
    return (np.round(np.clip(luma/count,0,1)*255).astype(np.uint8).reshape(edge,edge),
        (1-unsafe).reshape(edge,edge),(depths/count).astype(np.float32).reshape(edge,edge),
        mask.reshape(edge,edge),guard.reshape(edge,edge),transform)


def require_full_depth_plane(depth,domain,edge,max_error,*,check_active=lambda:None):
    """Fit/check all source support; proxy averaging cannot hide nonplanarity."""
    if (depth.dtype!=np.float32 or depth.ndim!=2 or domain.shape!=(edge,edge)
            or domain.dtype!=np.bool_ or not np.isfinite(max_error) or max_error<=0):
        raise ValueError('SEMANTIC_SCENE_DEPTH_INVALID')
    h,w=depth.shape
    normal=np.zeros((3,3),np.float64);rhs=np.zeros(3,np.float64);count=0
    def tiles():
        for top in range(0,h,256):
            for left in range(0,w,256):
                check_active();y=np.arange(top,min(top+256,h));x=np.arange(left,min(left+256,w))
                selected=domain[(y*edge//h)[:,None],(x*edge//w)[None,:]]
                yy,xx=np.nonzero(selected)
                if not len(xx):continue
                yy=yy+top;xx=xx+left;value=depth[yy,xx]
                if not np.isfinite(value).all() or np.any(value<0) or np.any(value>1):
                    raise ValueError('SEMANTIC_SCENE_DEPTH_INVALID')
                design=np.column_stack((xx/max(1,w-1),yy/max(1,h-1),np.ones(len(xx))))
                yield design,value
    for design,value in tiles():
        normal+=design.T@design;rhs+=design.T@value;count+=len(value)
    if count<32 or np.linalg.matrix_rank(normal)!=3:raise ValueError('SEMANTIC_SCENE_DEPTH_DEGENERATE')
    coefficients=np.linalg.solve(normal,rhs);check_active()
    for design,value in tiles():
        if np.any(np.abs(value-design@coefficients)>max_error):raise ValueError('SEMANTIC_SCENE_NONPLANAR')
    check_active()


def verify_actual_scene_footprint(source_mask,matrix,target_labels,target_protected,allowed,domain,edge,*,check_active=lambda:None):
    """Expand target authority using actual positive published-mask pixels."""
    from .semantic_transfer import iter_planar_mask_warp_tiles
    shape=target_labels.shape
    if target_protected.shape!=shape or domain.shape!=(edge,edge) or domain.dtype!=np.bool_:
        raise ValueError('SEMANTIC_SCENE_PROXY_INVALID')
    allowed=np.array(sorted(allowed),np.uint16)
    for part,tile in iter_planar_mask_warp_tiles(source_mask,matrix,shape,check_active=check_active):
        positive=tile>0
        if not np.any(positive):continue
        if np.any(target_protected[part][positive]>0):raise ValueError('SEMANTIC_SCENE_PROTECTED')
        if not np.isin(target_labels[part][positive],allowed).all():raise ValueError('SEMANTIC_SCENE_FOREGROUND')
        yy,xx=np.nonzero(positive)
        yy+=part[0].start;xx+=part[1].start
        domain[yy*edge//shape[0],xx*edge//shape[1]]=True
    check_active()
    return domain


def verify_full_scene_arrays(reference,target,reference_labels,target_labels,
        reference_depth,target_depth,source_mask,source_protected,target_protected,
        *,policy,proxy_edge,match_error_px_max,match_inlier_min,check_active=lambda:None):
    """Use the SAME native correspondence as actual transfer, then scene gates.

    Returns exact source-coordinate homography for comparison, not a scene proof
    receipt. A SQL-qualified installer must authenticate all evidence and policy.
    """
    from .semantic_scene import ScenePolicy,verify_planar_scene_proxies
    from .semantic_transfer import match_planar_linear_images
    if not isinstance(policy,ScenePolicy) or type(proxy_edge) is not int or not 32<=proxy_edge<=1024:
        raise ValueError('SEMANTIC_SCENE_POLICY_INVALID')
    edge=min(proxy_edge,*reference.shape[:2],*target.shape[:2])
    if edge<32:raise ValueError('SEMANTIC_SCENE_PROXY_GRID_INVALID')
    a=project_scene_source(reference,reference_labels,reference_depth,source_mask,source_protected,
        edge,policy.background_labels,check_active=check_active)
    # Broadcast zero is readonly O(1) memory, not a fabricated protection field.
    zero=np.broadcast_to(np.zeros((),np.float32),target.shape[:2])
    b=project_scene_source(target,target_labels,target_depth,zero,target_protected,
        edge,policy.background_labels,check_active=check_active)
    match=match_planar_linear_images(reference,target,match_error_px_max=match_error_px_max,
        match_inlier_min=match_inlier_min,check_active=check_active)
    if match.reference_to_target is None:raise ValueError(match.reason or 'NO_PLANAR_CORRESPONDENCE')
    # Conjugate actual source-coordinate mapping through exact pixel-center
    # transforms. Never fit an unrelated proxy homography for scene validation.
    matrix=b[5]@match.reference_to_target@np.linalg.inv(a[5])
    if not np.isfinite(matrix).all() or abs(matrix[2,2])<=1e-12:
        raise ValueError('SEMANTIC_SCENE_TRANSFORM_INVALID')
    matrix=matrix/matrix[2,2]
    derived=ScenePolicy(frozenset({1}),policy.min_support,policy.flow_error_px,policy.homography_error_px,policy.depth_plane_error)
    target_domain=verify_planar_scene_proxies(a[0],b[0],a[1],b[1],a[2],b[2],a[3],a[4],b[4],matrix,
        policy=derived,check_active=check_active)
    # Proxy centers cannot bound an arbitrarily magnified/projective cell.
    # Visit the SAME sampler used to publish the actual destination mask.
    verify_actual_scene_footprint(source_mask,match.reference_to_target,target_labels,target_protected,
        policy.background_labels,target_domain,edge,check_active=check_active)
    require_full_depth_plane(reference_depth,a[3]>0,edge,policy.depth_plane_error,check_active=check_active)
    # The proxy gate marks every bilinear contributor of both the actual warp
    # and consistency flow. Expand those bins to every original depth sample;
    # unrelated foreground elsewhere on the target is not a planarity claim.
    require_full_depth_plane(target_depth,target_domain,edge,policy.depth_plane_error,check_active=check_active)
    check_active();return match.reference_to_target
