import assert from 'node:assert/strict'
import { afterEach, describe, it } from 'node:test'
import { ProviderStore } from '../../src/lib/commit-generation/provider-store'
import {
  ProviderError,
  ProviderSettings,
} from '../../src/lib/commit-generation/provider-types'
import { AsyncInMemoryStore } from '../helpers/stores'

const settings: ProviderSettings = {
  name: 'Fixture provider',
  format: 'openai-responses',
  baseUrl: 'https://old.example.invalid/v1',
  authKind: 'apiKey',
}
const credential = 'fake-request-context-credential'

function hasCode(code: string) {
  return (error: unknown) => {
    assert.ok(error instanceof ProviderError)
    assert.equal(error.code, code)
    assert.ok(
      !JSON.stringify(error).includes(credential),
      'a request-context failure contains no credential'
    )
    return true
  }
}

describe('ProviderStore immutable original-kernel request contexts', () => {
  afterEach(() => localStorage.clear())
  it('does not return a newly saved origin credential to an old provider request', async () => {
    const store = new ProviderStore(new AsyncInMemoryStore())
    const original = await store.saveProvider(settings, credential)
    const save = store.saveProvider(
      { ...original, baseUrl: 'https://new.example.invalid/v1' },
      'fake-new-origin-key'
    )
    const read = store.getSecret(original.id, original)
    await save
    await assert.rejects(read, hasCode('invalid-configuration'))
    assert.equal(
      store.getProvider(original.id)?.baseUrl,
      'https://new.example.invalid/v1'
    )
    assert.equal(
      await store.getSecret(original.id),
      'fake-new-origin-key',
      'the new context still resolves its own OS-store value'
    )
  })

  it('checks the expected catalog snapshot after an earlier queued secure save, not before its mutation queue', async t => {
    const secure = new AsyncInMemoryStore()
    const store = new ProviderStore(secure)
    const original = await store.saveProvider(settings, credential)
    const write = secure.setItem.bind(secure)
    let release!: () => void
    const hold = new Promise<void>(done => {
      release = done
    })
    t.mock.method(
      secure,
      'setItem',
      async (...args: Parameters<AsyncInMemoryStore['setItem']>) => {
        await hold
        await write(...args)
      }
    )
    const save = store.saveProvider(
      { ...original, baseUrl: 'https://new.example.invalid/v1' },
      'fake-new-origin-key'
    )
    const update = store.updateModels(
      original.id,
      [{ id: 'old-host-model', name: 'Old host model' }],
      original
    )
    release()
    await save
    await assert.rejects(update, hasCode('invalid-configuration'))
    assert.deepEqual(
      store.getProvider(original.id)?.models,
      [],
      'an old catalog cannot restore availability under new connection settings'
    )
  })

  it('checks cancellation inside the serialized catalog mutation and leaves the saved selection unchanged', async () => {
    const store = new ProviderStore(new AsyncInMemoryStore())
    const provider = await store.saveProvider(settings, credential)
    await store.updateModels(provider.id, [{ id: 'model', name: 'Model' }])
    await store.setModelEnabled(provider.id, 'model', true)
    await store.setCurrentSelection({
      providerId: provider.id,
      modelId: 'model',
    })
    const expected = store.getProvider(provider.id)!
    const controller = new AbortController()
    const pending = store.updateModels(
      provider.id,
      [],
      expected,
      controller.signal
    )
    controller.abort()
    await assert.rejects(pending, hasCode('cancelled'))
    assert.deepEqual(store.currentSelection, {
      providerId: provider.id,
      modelId: 'model',
    })
    assert.equal(store.getEnabledModels().length, 1)
  })
})
