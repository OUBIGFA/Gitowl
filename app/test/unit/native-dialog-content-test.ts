import assert from 'node:assert/strict'
import { after, describe, it } from 'node:test'
import { access } from 'fs/promises'
import { resolve } from 'path'
import { Account } from '../../src/models/account'
import { ErrorWithMetadata } from '../../src/lib/error-with-metadata'
import { CopilotError } from '../../src/lib/copilot-error'
import {
  getUnderlyingError,
  isRawGitError,
} from '../../src/lib/underlying-error'
import {
  displayDialogURL,
  displayDialogDiagnostic,
} from '../../src/ui/lib/dialog-url'
import { getIconPreviewCatalog } from '../../src/ui/octicons/icon-preview-catalog'
import {
  getTestCLIAction,
  checkAboutTestUpdates,
} from '../../src/ui/lib/dialog-development-actions'
import { UpdateStatus } from '../../src/ui/lib/update-store'
import {
  updatedRepositoryCloneURL,
  parseRepositoryCloneIdentifier,
} from '../../src/ui/lib/repository-clone'

after(() => window.close())

describe('native dialog leaf projections', () => {
  it('imports every partition without touching Canvas, mounting a Web view or adding body nodes', async t => {
    let canvases = 0
    const before = document.body.childNodes.length
    const descriptor = Object.getOwnPropertyDescriptor(
      window.HTMLCanvasElement.prototype,
      'getContext'
    )!
    Object.defineProperty(window.HTMLCanvasElement.prototype, 'getContext', {
      configurable: true,
      value: () => {
        canvases++
        throw new Error('Native presenter must not initialize Canvas')
      },
    })
    t.after(() =>
      Object.defineProperty(
        window.HTMLCanvasElement.prototype,
        'getContext',
        descriptor
      )
    )
    await Promise.all([
      import('../../src/native-kernel/operation-dialogs'),
      import('../../src/native-kernel/repository-dialogs'),
      import('../../src/native-kernel/settings-dialogs'),
      import('../../src/native-kernel/auxiliary-dialogs'),
    ])
    assert.equal(
      canvases,
      0,
      'A nonvisual controller imported Terminal/xterm or another Web renderer'
    )
    assert.equal(
      document.body.childNodes.length,
      before,
      'Importing a presenter mounted Web UI'
    )
    assert.equal(
      document.querySelectorAll('canvas, iframe, webview, dialog').length,
      0
    )
  })

  it('covers the exact original popup union once, retaining the twelve typed credential/error contracts', async () => {
    const [operation, repository, auxiliary, settings, typed, model] =
      await Promise.all([
        import('../../src/native-kernel/operation-dialogs'),
        import('../../src/native-kernel/repository-dialogs'),
        import('../../src/native-kernel/auxiliary-dialogs'),
        import('../../src/native-kernel/settings-dialogs'),
        import('../../src/native-kernel/popup-presenter'),
        import('../../src/models/popup'),
      ])
    const types = [
      ...operation.operationDialogs.types,
      ...repository.repositoryDialogs.types,
      ...auxiliary.auxiliaryDialogs.types,
      ...settings.settingsDialogs.types,
      ...typed.supportedKernelPopupTypes,
    ]
    assert.equal(types.length, 97)
    assert.equal(
      new Set(types).size,
      types.length,
      'A popup has multiple continuation owners'
    )
    assert.deepEqual([...types].sort(), Object.values(model.PopupType).sort())
    assert.equal(typed.supportedKernelPopupTypes.length, 12)
  })

  it('redacts complete and unfinished credential URLs while preserving local paths and safe Git host/path data', () => {
    for (const [input, expected] of [
      [
        'https://user:URL-SENTINEL@host.invalid/repo.git?token=QUERY-SENTINEL#code=CODE-SENTINEL',
        'https://host.invalid/repo.git',
      ],
      [
        'ssh://user:SSH-SENTINEL@host.invalid/owner/repo.git',
        'ssh://host.invalid/owner/repo.git',
      ],
      [
        'https:user:URL-SENTINEL@host.invalid/repo.git',
        'https://host.invalid/repo.git',
      ],
      [
        '//user:URL-SENTINEL@host.invalid/repo?token=QUERY-SENTINEL',
        '//host.invalid/repo',
      ],
      ['https://user:URL-SENTINEL@', 'https://'],
      ['git@host.invalid:owner/repo.git', 'host.invalid:owner/repo.git'],
      ['E:\\local\\repo', 'E:\\local\\repo'],
      ['owner/repo', 'owner/repo'],
    ]) {
      assert.equal(displayDialogURL(input), expected, input)
      assert.doesNotMatch(displayDialogURL(input), /SENTINEL/)
    }
    const account = new Account(
      'fixture',
      'https://api.example.invalid',
      'ACCOUNT-SENTINEL',
      [],
      '',
      1,
      'Fixture'
    )
    const original =
      'Git rejected https://user:PASSWORD-SENTINEL@host.invalid/repo?code=OAUTH-SENTINEL using ACCOUNT-SENTINEL'
    assert.equal(
      displayDialogDiagnostic(original, [account]),
      'Git rejected https://host.invalid/repo using [redacted]'
    )
    assert.equal(
      displayDialogDiagnostic(
        'Git rejected https:user:PASSWORD-SENTINEL@host.invalid/repo',
        [account]
      ),
      'Git rejected https://host.invalid/repo'
    )
    assert.match(
      original,
      /PASSWORD-SENTINEL/,
      'Display redaction mutated continuation data'
    )
  })

  it('keeps authentication query data out of every shared clone-directory suggestion without changing the Git URL', () => {
    for (const url of [
      'https://user:PASSWORD-SENTINEL@host.invalid/owner/repo.git?token=QUERY-SENTINEL#oauth=OAUTH-SENTINEL',
      'git@host.invalid:owner/repo.git?token=QUERY-SENTINEL',
      'owner/repo?token=QUERY-SENTINEL',
    ]) {
      const update = updatedRepositoryCloneURL(
        url,
        resolve('_temp/native-full-ui'),
        null
      )
      assert.equal(update.url, url)
      assert.equal(update.path, resolve('_temp/native-full-ui/repo'))
      assert.doesNotMatch(
        JSON.stringify(update.lastParsedIdentifier),
        /SENTINEL/
      )
      assert.equal(parseRepositoryCloneIdentifier(url)?.name, 'repo')
    }
  })

  it('preserves original nested Copilot error precedence and underlying identities', () => {
    const copilot = new CopilotError('Original failure', 402)
    const wrapper = new ErrorWithMetadata(
      new Error('Wrapper', { cause: copilot }),
      {}
    )
    assert.equal(getUnderlyingError(wrapper), copilot)
    const ordinary = new Error('Original ordinary error')
    assert.equal(
      getUnderlyingError(new ErrorWithMetadata(ordinary, {})),
      ordinary
    )
    assert.equal(isRawGitError(ordinary), false)
    assert.equal(isRawGitError(null), false)
  })

  it('provides only original locked Octicons basename IDs and dimensions for all real assets', async () => {
    const catalog = getIconPreviewCatalog()
    assert.equal(catalog.length, 733)
    assert.equal(new Set(catalog.map(icon => icon.id)).size, catalog.length)
    assert.deepEqual(
      [...new Set(catalog.map(icon => icon.height))].sort((a, b) => a - b),
      [12, 16, 24, 48, 96]
    )
    await Promise.all(
      catalog.map(icon =>
        access(
          resolve('node_modules/@primer/octicons/build/svg', `${icon.id}.svg`)
        )
      )
    )
    for (const icon of catalog) {
      assert.deepEqual(Object.keys(icon).sort(), [
        'height',
        'id',
        'name',
        'width',
      ])
    }
    assert.doesNotMatch(
      JSON.stringify(catalog),
      /<svg|https:|\.svg|\"p\":|\"path\":/
    )
    assert.ok(
      catalog.some(
        icon =>
          icon.id === 'alert-16' && icon.height === 16 && icon.width === 16
      )
    )
  })

  it('shares exact original CLI trimming and optional branch semantics', () => {
    assert.equal(getTestCLIAction('open-repository', '  ', '', ''), null)
    assert.deepEqual(getTestCLIAction('open-repository', ' repo ', '', ''), {
      kind: 'open-repository',
      path: 'repo',
    })
    assert.deepEqual(
      getTestCLIAction(
        'clone-url',
        '',
        ' https://example.invalid/repo ',
        ' main '
      ),
      { kind: 'clone-url', url: 'https://example.invalid/repo', branch: 'main' }
    )
    assert.deepEqual(
      getTestCLIAction('clone-url', '', ' https://example.invalid/repo ', '  '),
      {
        kind: 'clone-url',
        url: 'https://example.invalid/repo',
        branch: undefined,
      }
    )
  })

  it('simulates the original About test delays without invoking the real updater', async () => {
    const statuses: number[] = []
    const delays: number[] = []
    await checkAboutTestUpdates(
      state => {
        statuses.push(state.status!)
      },
      async milliseconds => {
        delays.push(milliseconds)
      }
    )
    assert.deepEqual(statuses, [
      UpdateStatus.CheckingForUpdates,
      UpdateStatus.UpdateAvailable,
      UpdateStatus.UpdateReady,
    ])
    assert.deepEqual(delays, [5000, 10000])
  })
})
