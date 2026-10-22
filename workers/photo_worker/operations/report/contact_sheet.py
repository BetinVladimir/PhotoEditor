"""Deterministic paginated contact-sheet PNGs using an artifact-locked font."""
from contextlib import ExitStack
import math
from PIL import Image,ImageDraw,ImageFont,UnidentifiedImageError
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.report import ContactPage,ContactSheet,ContactSheetParams
from photo_worker.runtime.artifacts import ArtifactConfig,TaskArtifacts
from photo_worker.runtime.executor import ExecutionContext,Operation,OperationFailure,OperationResult


def _fit(image:Image.Image,width:int,height:int)->Image.Image:
    image=image.convert('RGB');scale=min(width/image.width,height/image.height);size=(max(1,round(image.width*scale)),max(1,round(image.height*scale)))
    return image.resize(size,Image.Resampling.LANCZOS)


def execute(ctx:ExecutionContext,artifacts:TaskArtifacts)->OperationResult:
    ctx.check_active()
    try:
        p=artifacts.params(ContactSheetParams);required=[entry.preview for entry in p.entries]+[p.font]
        if ctx.lease.execution_profile.get('backend')!='cpu' or list(ctx.lease.command.inputs)!=required:raise ValueError('Contact sheet input binding')
    except ValueError as exc:raise OperationFailure('INPUT_INVALID') from exc
    per_page=p.columns*p.rows;page_count=math.ceil(len(p.entries)/per_page);width=p.columns*p.cell_width;height=p.rows*p.cell_height;ctx.progress(0,page_count+1);pages=[];written=0
    with ExitStack() as stack:
        font_local=stack.enter_context(artifacts.input(p.font,suffix='.ttf'))
        try:font=ImageFont.truetype(str(font_local.path),20,layout_engine=ImageFont.Layout.BASIC)
        except (OSError,ValueError) as exc:raise OperationFailure('INPUT_INVALID') from exc
        for page_index in range(page_count):
            ctx.check_active();start=page_index*per_page;entries=p.entries[start:start+per_page];canvas=Image.new('RGB',(width,height),'white');draw=ImageDraw.Draw(canvas)
            for offset,entry in enumerate(entries):
                ctx.check_active()
                try:
                    with artifacts.input(entry.preview,suffix={'image/jpeg':'.jpg','image/png':'.png','image/webp':'.webp'}[entry.media_type]) as local:
                        with Image.open(local.path) as source:thumb=_fit(source,p.cell_width-24,p.cell_height-58)
                except (OSError,UnidentifiedImageError,ValueError) as exc:raise OperationFailure('INPUT_INVALID') from exc
                col=offset%p.columns;row=offset//p.columns;x=col*p.cell_width+(p.cell_width-thumb.width)//2;y=row*p.cell_height+8;canvas.paste(thumb,(x,y));label=entry.label
                while label and draw.textbbox((0,0),label,font=font)[2]>p.cell_width-24:label=label[:-1]
                if label!=entry.label:label=label[:-1]+'…' if len(label)>1 else '…'
                draw.text((col*p.cell_width+12,row*p.cell_height+p.cell_height-38),label,fill='black',font=font)
            ctx.check_active()
            with artifacts.scratch.reserve(width*height*3+1048576) as reservation:
                path=reservation.path('contact-page','.png');canvas.save(path,format='PNG',optimize=True,compress_level=6);reservation.validate_file(path);ctx.check_active();ref=artifacts.stage(path,reservation,'contact_page','image/png');written+=path.stat().st_size
            pages.append(ContactPage(artifact=ref,page=page_index+1,first_index=start,item_count=len(entries),width=width,height=height));ctx.progress(page_index+1,page_count+1)
    ctx.check_active()
    manifest=ContactSheet(locale=p.locale,font_lock_id=p.font_lock_id,total_items=len(p.entries),pages=pages);manifest_ref=artifacts.stage_contract(manifest,'contact_sheet');ctx.progress(page_count+1,page_count+1);return OperationResult(outputs=[*[v.artifact for v in pages],manifest_ref],bytes_written=written+len(canonical_bytes(manifest)))


def contact_sheet_operations(config:ArtifactConfig):
    def run(ctx):
        with TaskArtifacts.create(ctx,config,frozenset({'contact_page','contact_sheet'})) as a:return execute(ctx,a)
    return [Operation('report.contact_sheet','media',run)]
