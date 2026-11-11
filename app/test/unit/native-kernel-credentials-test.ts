import assert from 'node:assert/strict'
import { after, before, describe, it, mock } from 'node:test'
import { Account } from '../../src/models/account'
import { getKeyForAccount } from '../../src/lib/auth'
import { InMemoryStore } from '../helpers/stores'

// Test the original account and SSH flows against an isolated keytar boundary;
// no test creates, reads, migrates or deletes an actual OS credential.
const passwords = new Map<string, string>()
const calls: Array<{ method: string; key: string; login: string }> = []
const entry = (key: string, login: string) => JSON.stringify([key, login])
mock.module('keytar', {
  namedExports: {
    getPassword: async (key: string, login: string) => {
      calls.push({ method: 'get', key, login })
      return passwords.get(entry(key, login)) ?? null
    },
    setPassword: async (key: string, login: string, value: string) => {
      calls.push({ method: 'set', key, login })
      passwords.set(entry(key, login), value)
    },
    deletePassword: async (key: string, login: string) => {
      calls.push({ method: 'delete', key, login })
      return passwords.delete(entry(key, login))
    },
  },
})
let TokenStore: typeof import('../../src/lib/stores/token-store').TokenStore
let AccountsStore: typeof import('../../src/lib/stores/accounts-store').AccountsStore
let ssh: typeof import('../../src/lib/ssh/ssh-credential-storage')
const originalMode = process.env.GITOWL_NATIVE_CORE
const originalNamespace = process.env.GITOWL_CREDENTIAL_NAMESPACE
const namespace = 'io.github.oubigfa.gitowl.test.fixture'
const isolated = (key: string) => `${namespace} :: ${key}`

before(async () => {
  ;({ TokenStore } = await import('../../src/lib/stores/token-store'))
  ;({ AccountsStore } = await import('../../src/lib/stores/accounts-store'))
  ssh = await import('../../src/lib/ssh/ssh-credential-storage')
})
after(() => {
  if (originalMode === undefined) {
    delete process.env.GITOWL_NATIVE_CORE
  } else {
    process.env.GITOWL_NATIVE_CORE = originalMode
  }
  if (originalNamespace === undefined) {
    delete process.env.GITOWL_CREDENTIAL_NAMESPACE
  } else {
    process.env.GITOWL_CREDENTIAL_NAMESPACE = originalNamespace
  }
})

function nativeMode() {
  process.env.GITOWL_NATIVE_CORE = '1'
  process.env.GITOWL_CREDENTIAL_NAMESPACE = namespace
  passwords.clear()
  calls.length = 0
}

describe('native kernel secure-storage identity', () => {
  it('leaves the ordinary upstream keytar contract unchanged', async () => {
    delete process.env.GITOWL_NATIVE_CORE
    process.env.GITOWL_CREDENTIAL_NAMESPACE = namespace
    passwords.clear()
    calls.length = 0
    await TokenStore.setItem(
      'GitHub - https://api.example.invalid',
      'alice',
      'upstream'
    )
    assert.equal(
      await TokenStore.getItem('GitHub - https://api.example.invalid', 'alice'),
      'upstream'
    )
    assert.equal(
      await TokenStore.deleteItem(
        'GitHub - https://api.example.invalid',
        'alice'
      ),
      true
    )
    assert.ok(
      calls.every(call => call.key === 'GitHub - https://api.example.invalid')
    )
  })

  it('reads, stores and signs out original account sessions without touching upstream sentinel entries', async () => {
    nativeMode()
    const account = new Account(
      'alice',
      'https://api.example.invalid',
      'native-token',
      [],
      '',
      1,
      'Alice'
    )
    const key = getKeyForAccount(account)
    passwords.set(entry(key, account.login), 'UPSTREAM-SENTINEL')
    const data = new InMemoryStore()
    data.setItem('users', JSON.stringify([account.withToken('')]))
    const store = new AccountsStore(data, TokenStore)
    const metadataOnly = await store.getAll()
    assert.equal(
      metadataOnly[0].token,
      '',
      'original metadata may remain, but no Desktop credential is imported'
    )
    const added = await store.addAccount(account)
    assert.ok(added)
    assert.equal(
      passwords.get(entry(isolated(key), account.login)),
      'native-token'
    )
    const restarted = new AccountsStore(data, TokenStore)
    const restored = await restarted.getAll()
    assert.equal(
      restored[0].token,
      'native-token',
      'original session serialization still restores Gitowl credentials'
    )
    await restarted.removeAccount(restored[0])
    assert.equal(passwords.has(entry(isolated(key), account.login)), false)
    assert.equal(passwords.get(entry(key, account.login)), 'UPSTREAM-SENTINEL')
    assert.ok(
      calls.every(call => call.key.startsWith(namespace + ' :: ')),
      'no OS credential operation used an upstream service name'
    )
  })

  it('keeps original SSH success/failure credential cleanup inside the Gitowl namespace', async () => {
    nativeMode()
    const key = ssh.getSSHCredentialStoreKey('ssh')
    passwords.set(entry(key, 'key-hash'), 'UPSTREAM-SSH-SENTINEL')
    assert.equal(await TokenStore.getItem(key, 'key-hash'), null)
    await ssh.setSSHCredential(
      'operation',
      key,
      'key-hash',
      'native-passphrase'
    )
    assert.equal(await TokenStore.getItem(key, 'key-hash'), 'native-passphrase')
    await ssh.deleteMostRecentSSHCredential('operation')
    ssh.removeMostRecentSSHCredential('operation')
    assert.equal(passwords.has(entry(isolated(key), 'key-hash')), false)
    assert.equal(passwords.get(entry(key, 'key-hash')), 'UPSTREAM-SSH-SENTINEL')
    assert.ok(calls.every(call => call.key === isolated(key)))
  })

  it('rejects a malformed native launch rather than falling back to upstream secrets', () => {
    nativeMode()
    delete process.env.GITOWL_CREDENTIAL_NAMESPACE
    assert.throws(
      () => TokenStore.getItem('GitHub', 'alice'),
      /Missing Gitowl credential namespace/
    )
    assert.throws(
      () => TokenStore.setItem('GitHub', 'alice', 'value'),
      /Missing Gitowl credential namespace/
    )
    assert.throws(
      () => TokenStore.deleteItem('GitHub', 'alice'),
      /Missing Gitowl credential namespace/
    )
    assert.deepEqual(calls, [])
  })
})
