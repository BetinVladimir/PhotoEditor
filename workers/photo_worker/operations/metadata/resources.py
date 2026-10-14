"""Metadata CPU reservation bounds shared with the Delivery planner/SQL gate."""
def validate_metadata_resources(profile,width,height,encoded_bytes):
    if (type(width) is not int or type(height) is not int or width<1 or height<1
            or width*height>200000000 or type(encoded_bytes) is not int
            or not 1<=encoded_bytes<=2147483648 or not isinstance(profile,dict)
            or profile.get('backend')!='cpu' or profile.get('renderer_version')!='v2.2'):
        raise ValueError('METADATA_RESOURCE_INVALID')
    resource=profile.get('resource_estimate')
    fields={'ram_bytes','vram_bytes','scratch_bytes','output_bytes','pixel_count'}
    if not isinstance(resource,dict) or set(resource)!=fields or any(
            type(value) is not int or not 0<=value<=2**53-1 for value in resource.values()):
        raise ValueError('METADATA_RESOURCE_INVALID')
    pixels=width*height
    if (resource['vram_bytes']!=0 or resource['pixel_count']!=pixels
            or resource['ram_bytes']<(256<<20)+pixels*64+encoded_bytes*4
            or resource['scratch_bytes']<encoded_bytes*3+(8<<20)
            or resource['output_bytes']!=encoded_bytes+(2<<20)):
        raise ValueError('METADATA_RESOURCE_INVALID')
    return resource['output_bytes']
