import assert from 'node:assert/strict'
import { after, before, describe, it, mock } from 'node:test'
import { realpath } from 'node:fs/promises'
import type { getAssistedCommitProtectedResources } from '../../../src/lib/git/repository-operation'

describe('grouped Git metadata directory discovery', () => {
  let resources: typeof getAssistedCommitProtectedResources
  let grouped: { stdout: string; exitCode: number }
  const calls: string[] = []
  let directory: string
  before(async () => {
    directory = await realpath('.')
    mock.module('../../../src/lib/git/core', {
      namedExports: {
        git: async (args: string[]) => {
          calls.push(args[0])
          if (
            args.includes('--absolute-git-dir') &&
            args.includes('--git-common-dir')
          ) {
            return grouped
          }
          return {
            exitCode: 0,
            stdout: args[0] === 'rev-parse' ? directory + '\n' : '',
            stderr: '',
          }
        },
      },
    })
    resources = (await import('../../../src/lib/git/repository-operation'))
      .getAssistedCommitProtectedResources
  })
  after(() => mock.restoreAll())
  for (const kind of ['supported', 'newline in path', 'unsupported options']) {
    it(kind, async () => {
      calls.length = 0
      grouped = {
        stdout:
          (kind === 'newline in path' ? 'part\n' : '') +
          (directory + '\n').repeat(3),
        exitCode: kind === 'unsupported options' ? 129 : 0,
      }
      const result = await resources(directory)
      assert.deepEqual(result.paths, Array(8).fill(directory))
      assert.deepEqual(result.objectDirectories, [directory])
      assert.deepEqual(
        calls,
        kind === 'supported'
          ? ['rev-parse', '-c']
          : ['rev-parse', 'rev-parse', 'rev-parse', 'rev-parse', '-c']
      )
    })
  }
})
