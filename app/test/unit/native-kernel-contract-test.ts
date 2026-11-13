import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { Account } from '../../src/models/account'
import { Commit, shortenSHA } from '../../src/models/commit'
import { CommitIdentity } from '../../src/models/commit-identity'
import {
  DiffSelection,
  DiffSelectionType,
  DiffType,
} from '../../src/models/diff'
import { TipState } from '../../src/models/tip'
import { Repository } from '../../src/models/repository'
import { GitHubRepository } from '../../src/models/github-repository'
import { Owner } from '../../src/models/owner'
import { CloningRepository } from '../../src/models/cloning-repository'
import { RepositoryStateCache } from '../../src/lib/stores/repository-state-cache'
import { updateStore } from '../../src/ui/lib/update-store'
import { PopupType } from '../../src/models/popup'
import {
  AppFileStatusKind,
  WorkingDirectoryFileChange,
  WorkingDirectoryStatus,
} from '../../src/models/status'
import { projectKernelState } from '../../src/native-kernel/state-projection'
import {
  setDateFormatPreference,
  setTimeFormatPreference,
} from '../../src/models/formatting-preferences'
import {
  KernelProtocolError,
  parseKernelCommand,
} from '../../src/native-kernel/command-validation'
import type { IAppState, IRepositoryState } from '../../src/lib/app-state'
import {
  ChangesSelectionKind,
  RepositorySectionTab,
  SelectionType,
} from '../../src/lib/app-state'

function textDiff(text: string) {
  return {
    kind: DiffType.Text as const,
    text,
    hunks: [],
    maxLineNumber: 0,
    hasHiddenBidiChars: false,
  }
}

function repositoryState(
  file: WorkingDirectoryFileChange,
  commit: Commit
): IRepositoryState {
  const cache = new RepositoryStateCache({ increment: async () => undefined })
  const base = cache.get(new Repository('C:/fixture', 8, null, false))
  return {
    ...base,
    commitSelection: {
      ...base.commitSelection,
      diff: textDiff('history diff'),
    },
    changesState: {
      ...base.changesState,
      workingDirectory: WorkingDirectoryStatus.fromFiles([file]),
      selection: {
        kind: ChangesSelectionKind.WorkingDirectory,
        selectedFileIDs: [file.id],
        diff: textDiff('text diff'),
      },
      commitMessage: {
        summary: 'summary',
        description: 'description',
        timestamp: 1,
      },
      conflictState: null,
    },
    compareState: { ...base.compareState, commitSHAs: [commit.sha] },
    selectedSection: RepositorySectionTab.History,
    branchesState: { ...base.branchesState, tip: { kind: TipState.Unknown } },
    commitLookup: new Map([[commit.sha, commit]]),
    worktrees: [
      {
        path: 'C:/worktree',
        head: commit.sha,
        branch: 'refs/heads/main',
        isDetached: false,
        type: 'linked',
        isLocked: false,
        isPrunable: false,
      },
    ],
    aheadBehind: { ahead: 2, behind: 1 },
    isCommitting: true,
    isGeneratingCommitMessage: false,
  }
}

function fixture(): Pick<
  IAppState,
  | 'accounts'
  | 'signInState'
  | 'repositories'
  | 'localRepositoryStateLookup'
  | 'selectedState'
  | 'allPopups'
  | 'currentPopup'
  | 'askForConfirmationOnForcePush'
  | 'currentBanner'
  | 'currentFoldout'
  | 'showWelcomeFlow'
  | 'errorCount'
  | 'updateState'
> {
  const repository = new Repository('C:/repo', 7, null, false, 'worktree')
  const file = new WorkingDirectoryFileChange(
    'README.md',
    { kind: AppFileStatusKind.Modified },
    DiffSelection.fromInitialSelection(DiffSelectionType.All)
  )
  const identity = new CommitIdentity(
    'Ada',
    'ada@example.com',
    new Date('2024-01-01T00:00:00.000Z')
  )
  const commit = new Commit(
    '0123456789abcdef',
    shortenSHA('0123456789abcdef'),
    'summary',
    'body',
    identity,
    identity,
    [],
    [],
    ['v1']
  )
  return {
    accounts: [
      new Account(
        'alice',
        'https://api.example.test',
        'TOKEN-SENTINEL',
        [],
        '',
        1,
        'Alice',
        'free',
        undefined,
        true
      ),
    ],
    signInState: null,
    repositories: [repository],
    localRepositoryStateLookup: new Map(),
    selectedState: {
      type: SelectionType.Repository,
      repository,
      state: repositoryState(file, commit),
    },
    allPopups: [
      { id: 3, type: PopupType.About },
      { type: PopupType.TermsAndConditions },
    ],
    currentPopup: { id: 3, type: PopupType.About },
    askForConfirmationOnForcePush: true,
    currentBanner: null,
    currentFoldout: null,
    showWelcomeFlow: false,
    errorCount: 0,
    updateState: updateStore.state,
  }
}

describe('native kernel contract', () => {
  it('projects the assigned account identity rather than the repository owner', () => {
    const app = fixture() as IAppState
    const repository = new Repository(
      'C:/organization-project',
      10,
      new GitHubRepository(
        'project',
        new Owner('organization', 'https://work.example/api/v3', 1),
        1
      ),
      false,
      null,
      {},
      false,
      undefined,
      undefined,
      'alice'
    )
    const snapshot = projectKernelState(
      { ...app, repositories: [repository] },
      1
    )
    assert.deepEqual(snapshot.repositories[0].account, {
      endpoint: 'https://work.example/api/v3',
      login: 'alice',
    })
    assert.equal(
      JSON.stringify(snapshot.repositories).includes('TOKEN-SENTINEL'),
      false
    )
  })

  it('refreshes cached history menus when tags, selection or local reachability change', () => {
    const app = fixture() as IAppState
    const selected = app.selectedState as Extract<
      NonNullable<IAppState['selectedState']>,
      { type: SelectionType.Repository }
    >
    const state = selected.state
    const sha = state.compareState.commitSHAs[0]
    const commit = state.commitLookup.get(sha)!
    const items = (value: IAppState) =>
      projectKernelState(value, 1).workspace!.menus.find(
        m => m.target.kind === 'commit' && m.target.id === sha
      )!.items
    assert.equal(
      items(app).find(i => i.labelKey === 'workspace.deleteTagNamed')?.enabled,
      false
    )
    const changed = {
      ...app,
      selectedState: {
        ...selected,
        state: { ...state, tagsToPush: ['v1'], localCommitSHAs: [sha] },
      },
    }
    assert.equal(
      items(changed).find(i => i.labelKey === 'workspace.deleteTagNamed')
        ?.enabled,
      true
    )
    assert.equal(
      items(changed).find(i => i.labelKey === 'workspace.undoCommit')?.enabled,
      true
    )
    // GitStore retains the lookup Map while replacing a commit's tag data.
    state.commitLookup.set(
      sha,
      new Commit(
        commit.sha,
        commit.shortSha,
        commit.summary,
        commit.body,
        commit.author,
        commit.committer,
        commit.parentSHAs,
        commit.trailers,
        []
      )
    )
    assert.equal(
      items(changed).some(i => i.labelKey === 'workspace.deleteTagNamed'),
      false
    )
    assert.equal(
      items(changed).find(i => i.labelKey === 'workspace.copyTags')?.enabled,
      false
    )
  })

  it('keeps a loaded history responsive during repeated focus snapshots', t => {
    const app = fixture() as IAppState
    assert.equal(app.selectedState!.type, SelectionType.Repository)
    const selected = app.selectedState as Extract<
      NonNullable<IAppState['selectedState']>,
      { type: SelectionType.Repository }
    >
    const commits = Array.from({ length: 10000 }, (_, i) => {
      const identity = new CommitIdentity(
        'History fixture',
        'history@example.invalid',
        new Date(Date.UTC(2024, 0, 1) - i * 60000)
      )
      const sha = i.toString(16).padStart(40, '0')
      return new Commit(
        sha,
        sha.slice(0, 7),
        `Commit ${i}`,
        '',
        identity,
        identity,
        [],
        [],
        []
      )
    })
    const state: IAppState = {
      ...app,
      selectedState: {
        ...selected,
        state: {
          ...selected.state,
          compareState: {
            ...selected.state.compareState,
            commitSHAs: commits.map(c => c.sha),
          },
          commitLookup: new Map(commits.map(c => [c.sha, c])),
        },
      },
    }
    projectKernelState(state, 1)
    const durations = Array.from({ length: 3 }, (_, i) => {
      const start = performance.now()
      const snapshot = projectKernelState(
        { ...state, appIsFocused: i % 2 === 0 },
        i + 2
      )
      assert.equal(snapshot.selected!.history.length, 10000)
      return performance.now() - start
    }).sort((a, b) => a - b)
    t.diagnostic(
      `10000 loaded commits: median focus snapshot ${durations[1].toFixed(
        1
      )} ms`
    )
    // This deliberately generous bound catches rebuilding every date/menu on
    // focus, not individual frame scheduling noise on a shared Windows runner.
    assert.ok(
      durations[1] < 100,
      `focus projection blocked for ${durations[1].toFixed(1)} ms`
    )
  })

  it('projects original clone progress without exporting the remote URL', () => {
    const state = fixture() as IAppState
    const repository = new CloningRepository(
      'C:/clone',
      'https://secret@example.invalid/team/project.git'
    )
    for (const value of [0, 0.45, 1]) {
      const snapshot = projectKernelState(
        {
          ...state,
          repositories: [repository],
          selectedState: {
            type: SelectionType.CloningRepository,
            repository,
            progress: {
              kind: 'clone',
              value,
              description: 'Receiving objects',
            },
          },
        },
        1
      )
      assert.equal(snapshot.selected, null)
      assert.deepEqual(snapshot.cloningRepository, {
        repository: {
          id: repository.id,
          path: repository.path,
          name: 'project',
          alias: null,
          missing: false,
          login: null,
          kind: 'cloning',
        },
        progress: { value, description: 'Receiving objects' },
      })
      assert.doesNotMatch(
        JSON.stringify(snapshot),
        /secret@example|team\/project\.git/
      )
    }
    assert.equal(projectKernelState(state, 2).cloningRepository, null)
  })
  it('filters project aliases with original fuzzy matching without losing the full repository list', () => {
    const app = fixture() as IAppState
    const first = new Repository('C:/alpha', 101, null, false, 'Release Notes')
    const second = new Repository('C:/beta', 102, null, false, 'Other Project')
    const state = {
      ...app,
      repositories: [first, second],
      repositoryFilterText: 'rln',
      localRepositoryStateLookup: new Map(),
      recentRepositories: [],
    }
    const snapshot = projectKernelState(state, 1)
    assert.deepEqual(snapshot.repositoryFilter, {
      text: 'rln',
      paths: ['C:/alpha'],
    })
    assert.equal(snapshot.repositories.length, 2)
    assert.equal(snapshot.selected?.repository.path, 'C:/repo')
    assert.deepEqual(
      projectKernelState({ ...state, repositoryFilterText: 'unmatched' }, 2)
        .repositoryFilter?.paths,
      []
    )
    assert.deepEqual(
      projectKernelState({ ...state, repositoryFilterText: '' }, 3)
        .repositoryFilter?.paths,
      ['C:/alpha', 'C:/beta']
    )
  })

  it('admits project filtering text and rejects reflected or malformed fields', () => {
    const command = { name: 'repositories.filter', text: '中文项目' }
    assert.deepEqual(parseKernelCommand(command), command)
    for (const value of [
      { ...command, text: null },
      { ...command, text: 'a\0b' },
      { ...command, method: 'removeRepository' },
    ]) {
      assert.throws(() => parseKernelCommand(value), KernelProtocolError)
    }
  })

  it('admits only a leased repository menu path, without reflected actions', () => {
    const command = {
      name: 'repository.menu',
      repository: { id: 2, path: 'C:/target' },
      lease: 'a'.repeat(64),
      path: [2, 0],
    }
    assert.deepEqual(parseKernelCommand(command), command)
    for (const value of [
      { ...command, path: [] },
      { ...command, path: [-1] },
      { ...command, path: Array(9).fill(0) },
      { ...command, path: [0.5] },
      { ...command, lease: 'old' },
      { ...command, login: 'injected' },
      {
        ...command,
        repository: { ...command.repository, url: 'https://example.invalid' },
      },
    ]) {
      assert.throws(() => parseKernelCommand(value), KernelProtocolError)
    }
  })

  it('projects original date preferences and localized relative history labels', t => {
    const oldDate = localStorage.getItem('dateFormat')
    const oldTime = localStorage.getItem('timeFormat')
    t.after(() => {
      for (const [key, value] of [
        ['dateFormat', oldDate],
        ['timeFormat', oldTime],
      ]) {
        if (value === null) {
          localStorage.removeItem(key!)
        } else {
          localStorage.setItem(key!, value!)
        }
      }
    })
    setDateFormatPreference('yyyy-MM-dd')
    setTimeFormatPreference('HH:mm')
    const app = {
      ...fixture(),
      preferAbsoluteDates: true,
    } as unknown as IAppState
    const date = new Date(2024, 0, 1, 8, 30)
    const selected = app.selectedState!
    assert.ok(selected.type === SelectionType.Repository)
    selected.state.commitLookup
      .values()
      .next()
      .value!.author.date.setTime(date.getTime())
    const absolute = projectKernelState(app, 1).selected!.history[0]
    assert.equal(absolute.dateLabels?.en.text, '2024-01-01 08:30')
    assert.equal(absolute.dateLabels?.['zh-Hans'].text, '2024-01-01 08:30')
    t.mock.method(Date, 'now', () => date.getTime() + 5 * 60 * 1000)
    const relative = projectKernelState(
      { ...app, preferAbsoluteDates: false },
      2
    ).selected!.history[0]
    assert.equal(relative.dateLabels?.en.text, '5 minutes ago')
    assert.equal(relative.dateLabels?.['zh-Hans'].text, '5分钟前')
    assert.equal(relative.dateLabels?.['zh-Hant'].text, '5 分鐘前')
    assert.equal(relative.dateRefreshAfter, 60000)
    assert.equal(relative.dateLabels?.en.absolute, '2024-01-01 08:30')
  })

  it('projects only presentation data and preserves ordered history and popups', () => {
    const snapshot = projectKernelState(fixture() as unknown as IAppState, 4)
    const serialized = JSON.stringify(snapshot)
    assert.equal(serialized.includes('TOKEN-SENTINEL'), false)
    assert.deepEqual(snapshot.repositories[0], {
      id: 7,
      path: 'C:/repo',
      name: 'repo',
      alias: 'worktree',
      missing: false,
      login: null,
      kind: 'repository',
    })
    assert.deepEqual(snapshot.popups, [
      { id: 3, type: PopupType.About },
      { id: null, type: PopupType.TermsAndConditions },
    ])
    assert.equal(snapshot.selected?.diff?.kind, 'text')
    assert.equal(snapshot.selected?.history[0].sha, '0123456789abcdef')
    assert.equal(snapshot.presentationComplete, false)
    assert.equal(
      Object.prototype.hasOwnProperty.call(snapshot, 'rawContents'),
      false
    )
  })

  it('accepts valid commands and rejects unknown, extra, partial, NUL, and unsafe inputs', () => {
    assert.deepEqual(
      parseKernelCommand({
        name: 'repository.files.select',
        repository: { id: 7, path: 'C:/repo' },
        fileIDs: ['M+README.md'],
      }),
      {
        name: 'repository.files.select',
        repository: { id: 7, path: 'C:/repo' },
        fileIDs: ['M+README.md'],
      }
    )
    assert.deepEqual(
      parseKernelCommand({
        name: 'repository.commit',
        repository: { id: 7, path: 'C:/repo' },
        context: {
          summary: 's',
          description: null,
          trailers: [
            { token: 'Co-Authored-By', value: 'Ada <ada@example.com>' },
          ],
        },
      }).name,
      'repository.commit'
    )
    for (const input of [
      { name: 'dispatcher.secret', value: 'TOKEN-SENTINEL' },
      { name: 'state', extra: true },
      { name: 'repositories.add' },
      { name: 'popup.close', popupID: Number.MAX_SAFE_INTEGER + 1 },
      { name: 'signIn.enterprise', endpoint: 'bad\0endpoint' },
      {
        name: 'repository.select',
        repository: { id: 1, path: 'C:/repo', token: 'TOKEN-SENTINEL' },
      },
    ]) {
      assert.throws(
        () => parseKernelCommand(input),
        (error: unknown) => {
          assert.ok(error instanceof KernelProtocolError)
          assert.equal(String(error.message).includes('TOKEN-SENTINEL'), false)
          return true
        }
      )
    }
  })
})
