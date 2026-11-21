import fixtures from '../../../references/native-image-codecs.json'
import assert from 'node:assert/strict'
import { after, describe, it } from 'node:test'
import { Image } from '../../src/models/diff'
import { projectKernelImage } from '../../src/native-kernel/image-presentation'
import { parseKernelCommand } from '../../src/native-kernel/command-validation'
import {
  projectKernelMediaOwner,
  readKernelMedia,
  withMediaProjection,
} from '../../src/native-kernel/media-presentation'

after(() => window.close())

describe('bounded original image presentation', () => {
  it('releases replaced owner media, retains covered media, and decodes exact contents rather than pooled backing bytes', async t => {
    t.after(() => withMediaProjection(() => null))
    const owner = {}
    const a = new Image(
      new Uint8Array([99, 98, 97]).buffer,
      fixtures.webp.contents,
      'image/webp',
      11
    )
    const b = new Image(
      new Uint8Array([99, 98, 97]).buffer,
      fixtures.webp.previous,
      'image/webp',
      12
    )
    const project = (image: Image) =>
      withMediaProjection(() => {
        projectKernelMediaOwner(owner)
        return projectKernelImage(image, owner)
      }, [owner])
    const first = project(a)
    const read = (id: string) =>
      readKernelMedia({
        name: 'media.frame.read',
        mediaID: id,
        frame: 0,
        offset: 0,
      })
    const frame = await read(first.mediaID!)
    assert.equal(frame.width, 17)
    assert.equal(frame.frames, 2)
    assert.equal(Buffer.from(frame.contents, 'base64')[0], 255)
    const refreshed = project(
      new Image(new ArrayBuffer(0), a.contents, a.mediaType, a.bytes)
    )
    assert.equal(refreshed.mediaID, first.mediaID)
    await read(first.mediaID!)
    const second = project(b)
    await assert.rejects(read(first.mediaID!), /no longer displayed/)
    await read(second.mediaID!)
    withMediaProjection(() => null, [owner])
    await read(second.mediaID!)
    withMediaProjection(() => {
      projectKernelMediaOwner(owner)
    }, [owner])
    await assert.rejects(read(second.mediaID!), /no longer displayed/)
  })

  it('keeps two 25 MiB sides out of snapshots, with stable opaque IDs', async () => {
    const bytes = new Uint8Array(25 * 1024 * 1024)
    const encoded = Buffer.from(bytes).toString('base64')
    const before = new Image(bytes.buffer, encoded, 'image/png', bytes.length)
    const after = new Image(
      bytes.buffer,
      encoded.slice(0, -4) + 'AQ==',
      'image/png',
      bytes.length
    )
    const project = () => ({
      previous: projectKernelImage(before),
      current: projectKernelImage(after),
    })
    const snapshot = withMediaProjection(project)
    assert.ok(JSON.stringify(snapshot).length < 1024)
    assert.equal(snapshot.previous.contents, '')
    assert.notEqual(snapshot.previous.mediaID, snapshot.current.mediaID)
    assert.deepEqual(withMediaProjection(project), snapshot)
    withMediaProjection(() => null)
    await assert.rejects(
      readKernelMedia({
        name: 'media.frame.read',
        mediaID: snapshot.previous.mediaID!,
        frame: 0,
        offset: 0,
      }),
      /no longer displayed/
    )
  })

  it('retains media while its original popup is covered, then revokes it', async () => {
    const owner = {}
    const image = new Image(new ArrayBuffer(0), '', 'image/svg+xml', 0)
    const view = withMediaProjection(
      () => projectKernelImage(image, owner),
      [owner]
    )
    withMediaProjection(() => null, [owner])
    const restored = withMediaProjection(
      () => projectKernelImage(image, owner),
      [owner]
    )
    assert.equal(restored.mediaID, view.mediaID)
    withMediaProjection(() => null)
    await assert.rejects(
      readKernelMedia({
        name: 'media.frame.read',
        mediaID: view.mediaID!,
        frame: 0,
        offset: 0,
      }),
      /no longer displayed/
    )
  })

  it('admits only bounded named frame requests, not arbitrary URLs/files/decoder options', () => {
    const request = {
      name: 'media.frame.read',
      mediaID: '12345678-1234-1234-1234-123456789abc',
      frame: 0,
      offset: 0,
    }
    assert.deepEqual(parseKernelCommand(request), request)
    for (const extra of [
      { url: 'https://example.invalid' },
      { path: 'C:/private' },
      { frame: -1 },
      { offset: Number.MAX_SAFE_INTEGER + 1 },
      { mediaID: '../../private' },
      { frame: 0.5 },
    ]) {
      assert.throws(() => parseKernelCommand({ ...request, ...extra }))
    }
  })
})
