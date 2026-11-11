"""Regenerate self-authored codec fixtures; tests consume JSON, not Pillow.

Optional authoring tool: Pillow 12.2.0 (https://python-pillow.org/).
No photographs, user paths or third-party artwork are included. DDS is the
original converter's DXT1 first-mipmap format, not an extension of its support.
"""
import base64
import io
import json
import struct
from pathlib import Path
from PIL import Image

if Image.__version__ != '12.2.0':
    raise RuntimeError('Use Pillow 12.2.0 to reproduce the checked codec inputs')
red = Image.new('RGBA', (17, 13), (255, 0, 0, 128))
blue = Image.new('RGBA', (17, 13), (0, 0, 255, 128))
fixtures = {}
encode = lambda data: base64.b64encode(data).decode('ascii')
for name, format, mime, options in [
    ('apng', 'PNG', 'image/png', dict(save_all=True, append_images=[blue], duration=[70, 110], loop=2, disposal=0, blend=0)),
    ('webp', 'WEBP', 'image/webp', dict(save_all=True, append_images=[blue], duration=[70, 110], loop=2, lossless=True)),
    ('ico', 'ICO', 'image/x-icon', dict(sizes=[(16, 16)])),
    ('avif', 'AVIF', 'image/avif', dict(quality=100, subsampling='4:4:4')),
]:
    current, previous = io.BytesIO(), io.BytesIO()
    a, b = (red.resize((16, 16)), blue.resize((16, 16))) if format == 'ICO' else (red, blue)
    a.save(current, format=format, **options)
    b.save(previous, format=format, **(dict(sizes=[(16, 16)]) if format == 'ICO' else {}))
    fixtures[name] = dict(mediaType=mime, contents=encode(current.getvalue()), previous=encode(previous.getvalue()), frames=2 if name in ('apng', 'webp') else 1, width=a.width, height=a.height)

exif = Image.Exif()
exif[274] = 6  # Original <img> and MyGo must display a clockwise rotation.
current, previous = io.BytesIO(), io.BytesIO()
red.convert('RGB').save(current, format='JPEG', quality=95, exif=exif)
blue.convert('RGB').save(previous, format='JPEG', quality=95, exif=exif)
fixtures['jpeg-exif'] = dict(mediaType='image/jpeg', contents=encode(current.getvalue()), previous=encode(previous.getvalue()), frames=1, width=13, height=17, opaque=True)

header = [0x20534444, 124, 0x81007, 4, 4, 8, 0, 1] + [0] * 11
header += [32, 4, 0x31545844, 0, 0, 0, 0, 0, 0x1000, 0, 0, 0, 0]
assert len(header) == 32
head = struct.pack('<32I', *header)
fixtures['dds'] = dict(mediaType='image/vnd-ms.dds', contents=encode(head + struct.pack('<HHI', 0xf800, 0, 0)), previous=encode(head + struct.pack('<HHI', 0x001f, 0, 0)), frames=1, width=4, height=4, opaque=True)

root = Path(__file__).resolve().parents[1]
(root / 'references/native-image-codecs.json').write_text(json.dumps(fixtures, indent=2) + '\n', encoding='utf-8')
