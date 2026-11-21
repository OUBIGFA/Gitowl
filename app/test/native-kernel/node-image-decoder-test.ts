import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import fixtures from '../../../references/native-image-codecs.json'
import { decodeImageFrame } from '../../src/native-kernel/node-image-decoder'

describe('production image decoding without Chromium', () => {
  for (const [name, fixture] of Object.entries(fixtures)) {
    it(`preserves ${name} geometry, colors and animation metadata`, async () => {
      const data = Buffer.from(fixture.contents, 'base64')
      const previous = await decodeImageFrame(
        Buffer.from(fixture.previous, 'base64'),
        0,
        fixture.mediaType
      )
      assert.equal(previous.width, fixture.width)
      assert.equal(previous.height, fixture.height)
      const first = await decodeImageFrame(data, 0, fixture.mediaType)
      assert.equal(first.width, fixture.width)
      assert.equal(first.height, fixture.height)
      assert.equal(first.frames, fixture.frames)
      assert.equal(first.data.length, fixture.width * fixture.height * 4)
      assert.ok(
        first.data[0] >= 245 && first.data[1] <= 10 && first.data[2] <= 10
      )
      const opaque = 'opaque' in fixture && fixture.opaque
      assert.ok(
        opaque
          ? first.data[3] === 255
          : first.data[3] >= 120 && first.data[3] <= 140
      )
      if (fixture.frames > 1) {
        const second = await decodeImageFrame(data, 1, fixture.mediaType)
        assert.equal(first.duration, 70000)
        assert.equal(second.duration, 110000)
        assert.equal(first.repetitions, 1)
        assert.ok(second.data[2] >= 245 && second.data[0] <= 10)
      }
    })
  }
  it('rejects malformed images and unavailable animation frames', async () => {
    await assert.rejects(decodeImageFrame(Buffer.from('invalid image'), 0))
    await assert.rejects(
      decodeImageFrame(Buffer.from(fixtures.webp.contents, 'base64'), 2),
      /frame/i
    )
  })
})
