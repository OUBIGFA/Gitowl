import assert from 'node:assert/strict'
import { EventEmitter } from 'events'
import { it, mock } from 'node:test'
import { resolve } from 'path'

const launches: Array<{ executable: string; args: string[] }> = []
mock.module('child_process', {
  namedExports: {
    execFile: () => assert.fail('Windows native CLI must not use macOS open'),
    spawn: (executable: string, args: string[]) => {
      launches.push({ executable, args })
      return Object.assign(new EventEmitter(), { unref: () => {} })
    },
  },
})

it(
  'the production CLI keeps open/clone/branch grammar and starts only Gitowl',
  { skip: process.platform !== 'win32' },
  async () => {
    const argv = process.argv
    const name = __APP_NAME__
    const runAsNode = process.env.ELECTRON_RUN_AS_NODE
    try {
      Object.assign(globalThis, { __APP_NAME__: 'Gitowl' })
      process.env.ELECTRON_RUN_AS_NODE = '1'
      process.argv = [
        'electron.exe',
        'cli.js',
        'clone',
        '-b',
        'feature with spaces',
        'owner/repository',
      ]
      await import('../../src/cli/main')
      assert.equal(launches.length, 1)
      assert.match(launches[0].executable, /[\\/]Gitowl\.exe$/)
      assert.deepEqual(launches[0].args, [
        '--cli-clone=https://github.com/owner/repository',
        '--cli-branch=feature with spaces',
      ])
      // Assert the executable chosen by the production parser itself. Its
      // source path has the same two-level depth as desktop-kernel/app/cli.js.
      assert.equal(
        launches[0].executable,
        resolve('app/Gitowl.exe')
      )
      assert.equal(process.env.ELECTRON_RUN_AS_NODE, undefined)
    } finally {
      process.argv = argv
      Object.assign(globalThis, { __APP_NAME__: name })
      if (runAsNode === undefined) {
        delete process.env.ELECTRON_RUN_AS_NODE
      } else {
        process.env.ELECTRON_RUN_AS_NODE = runAsNode
      }
    }
  }
)
