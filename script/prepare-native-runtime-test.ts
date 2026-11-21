import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { mkdir, readFile, writeFile } from 'fs/promises'
import { join } from 'path'
import { createTempDirectory } from '../app/test/helpers/temp'
import {
  hash,
  inventory,
  reusableRuntime,
  runtimeSourceInventory,
} from './prepare-native-runtime'
import { getReplacements } from '../app/app-info'
import nativeProduct from '../app/native/mygo.json'
import upstreamProduct from '../app/package.json'

describe('original Desktop runtime receipts', () => {
  it('does not invalidate production sources for test-only edits but rehashes runtime inputs', async context => {
    const root = await createTempDirectory(context)
    for (const directory of [
      'app/src',
      'app/test',
      'gemoji',
      'vendor',
      'app/static',
      'references',
      'third_party/licenses/native',
      'script/licenses',
      'script/validate-sass',
    ]) {
      await mkdir(join(root, directory), { recursive: true })
    }
    await writeFile(
      join(root, 'app/src/example.ts'),
      'export const value = 1\n'
    )
    await writeFile(
      join(root, 'app/test/example-test.ts'),
      'test version one\n'
    )
    const menuFile = join(root, 'references/native-ui-menu-labels.json')
    await writeFile(menuFile, '{"menu":"first"}\n')
    const before = await runtimeSourceInventory(root)
    await writeFile(
      join(root, 'app/test/example-test.ts'),
      'test version two\n'
    )
    assert.deepEqual(await runtimeSourceInventory(root), before)
    await writeFile(join(root, 'references/notes.md'), 'Documentation only\n')
    assert.deepEqual(await runtimeSourceInventory(root), before)
    await writeFile(menuFile, '{"menu":"second"}\n')
    assert.notDeepEqual(await runtimeSourceInventory(root), before)
    await writeFile(menuFile, '{"menu":"first"}\n')
    await writeFile(
      join(root, 'app/src/example.ts'),
      'export const value = 2\n'
    )
    assert.notDeepEqual(await runtimeSourceInventory(root), before)
  })
  it('registers Gitowl URL schemes and points the staged Windows CLI at MyGo', async () => {
    assert.deepEqual(nativeProduct.urlSchemes, ['gitowl', 'gitowl-auth'])
    const trampoline = await readFile(
      join(__dirname, 'native-windows-cli.bat'),
      'utf8'
    )
    assert.match(trampoline, /node\.exe/)
    assert.match(trampoline, /cli\.js/)
    assert.doesNotMatch(trampoline, /GitHubDesktop\.exe/i)
  })

  it('uses fixed Gitowl compile identity only for the explicit native build', t => {
    const previous = process.env.GITOWL_NATIVE_BUILD
    t.after(() => {
      if (previous === undefined) {
        delete process.env.GITOWL_NATIVE_BUILD
      } else {
        process.env.GITOWL_NATIVE_BUILD = previous
      }
    })
    process.env.GITOWL_NATIVE_BUILD = '1'
    const native = getReplacements()
    assert.equal(native.__APP_NAME__, JSON.stringify(nativeProduct.name))
    assert.equal(native.__APP_VERSION__, JSON.stringify(nativeProduct.version))
    delete process.env.GITOWL_NATIVE_BUILD
    const upstream = getReplacements()
    assert.equal(
      upstream.__APP_NAME__,
      JSON.stringify(upstreamProduct.productName)
    )
    assert.equal(
      upstream.__APP_VERSION__,
      JSON.stringify(upstreamProduct.version)
    )
  })
  it('uses canonical paths and hashes contents, not mtimes', async t => {
    const root = await createTempDirectory(t)
    await mkdir(join(root, 'git'))
    await writeFile(join(root, 'z.exe'), 'z')
    await writeFile(join(root, 'git', 'A.exe'), 'a')
    assert.deepEqual(await inventory(root), [
      { path: 'git/A.exe', sha256: hash('a') },
      { path: 'z.exe', sha256: hash('z') },
    ])
  })

  it('reuses only matching source/lock/binary inputs AND every staged byte', async t => {
    const root = await createTempDirectory(t)
    const runtime = join(root, 'desktop-kernel')
    await mkdir(runtime)
    await writeFile(join(runtime, 'electron.exe'), 'official dist fixture')
    await writeFile(join(runtime, 'LICENSE'), 'original license fixture')
    const receipt = join(root, 'runtime-receipt.json')
    const inputHash = hash('source + locks + registered binary trees')
    await writeFile(
      receipt,
      JSON.stringify({ inputHash, files: await inventory(runtime) })
    )
    assert.equal(await reusableRuntime(receipt, runtime, inputHash), true)
    assert.equal(
      await reusableRuntime(receipt, runtime, hash('changed source')),
      false
    )
    await writeFile(join(runtime, 'LICENSE'), 'changed license')
    assert.equal(await reusableRuntime(receipt, runtime, inputHash), false)
    await writeFile(join(runtime, 'LICENSE'), 'original license fixture')
    await writeFile(join(runtime, 'node.exe'), 'retired research runtime')
    assert.equal(await reusableRuntime(receipt, runtime, inputHash), false)
  })

  it('does not reuse missing output or accept malformed receipts', async t => {
    const root = await createTempDirectory(t)
    const receipt = join(root, 'runtime-receipt.json')
    const directory = join(root, 'desktop-kernel')
    assert.equal(await reusableRuntime(receipt, directory, 'input'), false)
    await writeFile(receipt, '{')
    await assert.rejects(
      reusableRuntime(receipt, directory, 'input'),
      SyntaxError
    )
    assert.equal(await readFile(receipt, 'utf8'), '{')
  })

  it('rejects Git metadata hidden from source inventories in a runtime', async t => {
    const root = await createTempDirectory(t)
    const runtime = join(root, 'desktop-kernel')
    await mkdir(runtime)
    await writeFile(join(runtime, 'electron.exe'), 'runtime fixture')
    const receipt = join(root, 'runtime-receipt.json')
    const inputHash = hash('runtime without private metadata')
    await writeFile(
      receipt,
      JSON.stringify({ inputHash, files: await inventory(runtime) })
    )
    await writeFile(join(runtime, '.git'), 'gitdir: private-workspace-path')
    assert.equal(await reusableRuntime(receipt, runtime, inputHash), false)
  })
})
