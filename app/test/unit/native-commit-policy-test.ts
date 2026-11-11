import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { RepoRulesInfo } from '../../src/models/repo-rules'
import {
  hasCommitRuleFailure,
  defaultCommitSummary,
} from '../../src/ui/changes/commit-policy'
import { parseCommitCommand } from '../../src/native-kernel/commit-protocol'
import { DiffSelection, DiffSelectionType } from '../../src/models/diff'
import {
  WorkingDirectoryFileChange,
  AppFileStatusKind,
} from '../../src/models/status'
import { authorFromUserHit } from '../../src/ui/lib/author-input/author-from-hit'
import { commitDialogContext } from '../../src/native-kernel/operation-dialogs'

// These are the shared original UI policies, not a second commit implementation.
describe('native commit shared policies', () => {
  it('retains bypass semantics, message-only assisted exemption and first-push branch gating', () => {
    const info = new RepoRulesInfo()
    const base = {
      enabled: true,
      info,
      message: 'pass' as const,
      author: 'pass' as const,
      branch: 'pass' as const,
      aheadBehind: { ahead: 1, behind: 0 },
    }
    for (const key of [
      'basicCommitWarning',
      'signedCommitsRequired',
      'pullRequestRequired',
    ] as const) {
      info[key] = true
      assert.equal(hasCommitRuleFailure(base), true, key)
      assert.equal(hasCommitRuleFailure({ ...base, enabled: false }), false)
      info[key] = 'bypass'
      assert.equal(hasCommitRuleFailure(base), false, key)
      info[key] = false
    }
    assert.equal(hasCommitRuleFailure({ ...base, message: 'fail' }), true)
    assert.equal(
      hasCommitRuleFailure({ ...base, message: 'fail' }, false),
      false
    )
    assert.equal(hasCommitRuleFailure({ ...base, author: 'fail' }, false), true)
    assert.equal(hasCommitRuleFailure({ ...base, branch: 'fail' }), false)
    assert.equal(
      hasCommitRuleFailure({ ...base, branch: 'fail', aheadBehind: null }),
      true
    )
    info.creationRestricted = true
    assert.equal(hasCommitRuleFailure(base), false)
    assert.equal(hasCommitRuleFailure({ ...base, aheadBehind: null }), true)
    info.creationRestricted = 'bypass'
    assert.equal(
      hasCommitRuleFailure({
        ...base,
        aheadBehind: null,
        branch: 'bypass',
        author: 'bypass',
        message: 'bypass',
      }),
      false
    )
  })

  it('keeps original basename/default-summary semantics and only known co-author trailers', () => {
    for (const [kind, verb] of [
      [AppFileStatusKind.New, 'Create'],
      [AppFileStatusKind.Untracked, 'Create'],
      [AppFileStatusKind.Deleted, 'Delete'],
      [AppFileStatusKind.Modified, 'Update'],
    ] as const) {
      const file = new WorkingDirectoryFileChange(
        'folder/file.txt',
        { kind },
        DiffSelection.fromInitialSelection(DiffSelectionType.All)
      )
      assert.equal(defaultCommitSummary([file], true), `${verb} file.txt`)
      assert.equal(defaultCommitSummary([file], false), 'Summary (required)')
    }
    const author = authorFromUserHit({
      kind: 'known-user',
      username: 'person',
      name: null,
      email: '',
      endpoint: 'https://api.github.com',
    })
    assert.deepEqual(author, {
      kind: 'known',
      username: 'person',
      name: 'person',
      email: 'person@users.noreply.github.com',
    })
    const message = {
      summary: 'summary',
      description: 'description',
      generatedByCopilot: true,
      timestamp: 1,
    }
    assert.deepEqual(
      commitDialogContext(
        message,
        [author, { kind: 'unknown', username: 'missing', state: 'error' }],
        true,
        true
      ),
      {
        summary: 'summary',
        description: 'description',
        trailers: [
          {
            token: 'Co-Authored-By',
            value: 'person <person@users.noreply.github.com>',
          },
        ],
        amend: true,
        messageGeneratedByCopilot: true,
      }
    )
    assert.deepEqual(
      commitDialogContext(message, [author], false, false).trailers,
      []
    )
  })

  it('rejects untyped options and injected identities/callbacks in the finite command protocol', () => {
    const repository = { id: 1, path: '/fixture' }
    assert.deepEqual(
      parseCommitCommand({
        name: 'commit.options',
        repository,
        options: { signOffCommits: true },
      }),
      { name: 'commit.options', repository, options: { signOffCommits: true } }
    )
    for (const command of [
      { name: 'commit.options', options: { signOffCommits: 'true' } },
      { name: 'commit.options', options: { author: 'forged' } },
      { name: 'commit.coAuthors.add', id: '1', email: 'forged' },
      { name: 'commit.assisted.create', onAccepted: 'invoke' },
      { name: 'commit.assisted.retry', runID: 'run', kind: 'anything' },
      { name: 'commit.coAuthors.search', query: 'bad\0query' },
    ]) {
      assert.throws(
        () => parseCommitCommand({ repository, ...command }),
        /Invalid/
      )
    }
    assert.equal(
      parseCommitCommand({ name: 'dispatcher.arbitrary', repository }),
      null
    )
  })
})
