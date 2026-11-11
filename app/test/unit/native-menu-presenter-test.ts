import assert from 'node:assert/strict'
import { after, before, describe, it } from 'node:test'
import { EventEmitter } from 'events'
import { ipcRenderer } from 'electron'
import {
  NativeMenuPresenter,
  KernelMenuItem,
} from '../../src/native-kernel/menu-presenter'
import { buildDefaultMenuTemplate } from '../../src/main-process/menu/build-default-menu'
import { executableMenuItem } from '../../src/main-process/menu/executable-menu-item'
import { menuFromElectronMenu, IMenu } from '../../src/models/app-menu'
import type { MenuLabelsEvent } from '../../src/models/menu-labels'
import { PopupType } from '../../src/models/popup'
import { Shell } from '../../src/lib/shells/win32'
import { IAppState, SelectionType } from '../../src/lib/app-state'
import { Repository } from '../../src/models/repository'
import { Branch, BranchType } from '../../src/models/branch'
import { TipState } from '../../src/models/tip'
import labels from '../../../references/native-ui-menu-labels.json'

const ipc = new EventEmitter()
const sent: unknown[][] = []
Object.assign(ipcRenderer, {
  send: (...args: unknown[]) => sent.push(args),
  on: ipc.on.bind(ipc),
  once: ipc.once.bind(ipc),
  removeListener: ipc.removeListener.bind(ipc),
})
before(() => {
  localStorage.clear()
  localStorage.setItem('stats-opt-out', '1')
  localStorage.setItem('has-sent-stats-opt-in-ping', '1')
})
after(() => window.close())

// Electron is the only replaced boundary. The production template, projection,
// AppMenu model, live state policy, runtime, Dispatcher and popup stack are real.
function electronMenu(
  template: ReadonlyArray<Electron.MenuItemConstructorOptions>
): Electron.Menu {
  return {
    items: template.map(item => ({
      ...item,
      enabled: item.enabled ?? true,
      visible: item.visible ?? true,
      label: item.label ?? '',
      type: item.type ?? (item.submenu ? 'submenu' : 'normal'),
      submenu: Array.isArray(item.submenu)
        ? electronMenu(item.submenu)
        : undefined,
    })),
  } as unknown as Electron.Menu
}
function flatten(items: ReadonlyArray<KernelMenuItem>): KernelMenuItem[] {
  return items.flatMap(item => [item, ...flatten(item.items ?? [])])
}
const base: MenuLabelsEvent = {
  selectedShell: null,
  selectedExternalEditor: null,
  askForConfirmationOnForcePush: false,
  askForConfirmationOnRepositoryRemoval: false,
}

async function fixture() {
  const { getAppRuntime } = await import('../../src/ui/app-runtime')
  const runtime = getAppRuntime()
  const presenter = new NativeMenuPresenter(runtime)
  const setMenu = (menu: IMenu) => ipc.emit('app-menu', {}, menu)
  setMenu(menuFromElectronMenu(electronMenu(buildDefaultMenuTemplate(base))))
  return { runtime, presenter, setMenu }
}

describe('original application menu in the native presenter', () => {
  it('covers every original platform/test/dynamic label with all three catalogs', async () => {
    const { runtime, presenter, setMenu } = await fixture()
    const platform = { __WIN32__, __DARWIN__, __LINUX__, __DEV__ }
    try {
      for (const os of ['windows', 'mac', 'linux']) {
        Object.assign(globalThis, {
          __WIN32__: os === 'windows',
          __DARWIN__: os === 'mac',
          __LINUX__: os === 'linux',
          __DEV__: true,
        })
        for (let bits = 0; bits < 128; bits++) {
          const template = buildDefaultMenuTemplate({
            ...base,
            isStashedChangesVisible: !!(bits & 1),
            isChangesFilterVisible: !!(bits & 2),
            hasCurrentPullRequest: !!(bits & 4),
            askForConfirmationOnRepositoryRemoval: !!(bits & 8),
            askForConfirmationWhenStashingAllChanges: !!(bits & 16),
            isForcePushForCurrentRepository: !!(bits & 32),
            askForConfirmationOnForcePush: !!(bits & 64),
            selectedShell: bits % 2 ? Shell.PowerShell : null,
            selectedExternalEditor: bits % 2 ? 'Editor' : null,
            contributionTargetDefaultBranch:
              bits % 2 ? 'feature/a&b' : undefined,
          })
          setMenu(menuFromElectronMenu(electronMenu(template)))
          const items = flatten(presenter.getState().items)
          const original = (menu: IMenu): string[] =>
            menu.items.flatMap(item => [
              item.id,
              ...(item.type === 'submenuItem' ? original(item.menu) : []),
            ])
          assert.deepEqual(
            items.map(item => item.id),
            original(runtime.appStore.getState().appMenuState[0])
          )
          for (const item of items.filter(i => i.type !== 'separator')) {
            const translations = labels[item.labelKey as keyof typeof labels]
            assert.ok(translations, `${os}: ${item.id} -> ${item.labelKey}`)
            for (const locale of ['en', 'zh-Hans', 'zh-Hant'] as const) {
              assert.ok(translations[locale].length > 0)
            }
          }
          assert.equal(
            items.find(i => i.id === 'push')?.accelerator,
            'CmdOrCtrl+P'
          )
          assert.equal(
            items.find(i => i.id === 'create-pull-request')?.accelerator,
            'CmdOrCtrl+R'
          )
          assert.ok(items.some(i => i.role === 'undo'))
          assert.ok(
            items.some(
              i => i.labelKey === 'menu.select-all' && i.role === 'selectAll'
            ),
            'Select All must edit the native focused control before list shortcuts'
          )
        }
      }
    } finally {
      Object.assign(globalThis, platform)
      presenter.dispose()
    }
  })

  it('uses the original Dispatcher, rejects stale/hidden/container IDs and preserves popup guards', async () => {
    const { runtime, presenter, setMenu } = await fixture()
    await runtime.dispatcher.endWelcomeFlow()
    sent.length = 0
    await presenter.execute('new-repository')
    assert.ok(
      sent.some(
        args =>
          args[0] === 'execute-menu-item-by-id' && args[1] === 'new-repository'
      )
    )
    await presenter.handleEvent('create-repository')
    assert.equal(
      runtime.appStore.getState().currentPopup?.type,
      PopupType.CreateRepository
    )
    await assert.rejects(
      presenter.execute('new-repository'),
      /no longer available/
    )
    await assert.rejects(
      presenter.execute('NOT-A-MENU-ID'),
      /no longer available/
    )
    await runtime.dispatcher.closePopup(PopupType.CreateRepository)
    const menu = menuFromElectronMenu(
      electronMenu(buildDefaultMenuTemplate(base))
    )
    await assert.rejects(
      presenter.execute(menu.items[0].id),
      /no longer available/
    )
    setMenu({
      ...menu,
      items: menu.items.map(item => ({ ...item, visible: false })),
    })
    await assert.rejects(
      presenter.execute('new-repository'),
      /no longer available/
    )
    presenter.dispose()
  })

  it('re-evaluates original repository, network, operation, welcome and popup states before a click', async t => {
    const { runtime, presenter } = await fixture()
    const original = runtime.appStore.getState()
    const repository = new Repository('C:/fixture/native-menu', 42, null, false)
    const cached = runtime.repositoryStateManager.get(repository)
    const branch = new Branch(
      'feature',
      'origin/feature',
      { sha: 'a'.repeat(40) },
      BranchType.Local,
      'refs/heads/feature'
    )
    const selected = {
      ...cached,
      branchesState: {
        ...cached.branchesState,
        tip: { kind: TipState.Valid as const, branch },
      },
      remote: { name: 'origin', url: 'https://example.invalid/repository' },
    }
    const ready: IAppState = {
      ...original,
      showWelcomeFlow: false,
      currentPopup: null,
      windowState: 'normal',
      repositories: [repository],
      selectedState: {
        type: SelectionType.Repository,
        repository,
        state: selected,
      },
    }
    let state = ready
    t.mock.method(runtime.appStore, 'getState', () => state)
    const enabled = (id: string) =>
      flatten(presenter.getState().items).find(item => item.id === id)?.enabled
    assert.equal(enabled('push'), true)
    assert.equal(enabled('pull'), true)
    assert.equal(enabled('fetch'), true)
    state = {
      ...ready,
      selectedState: {
        type: SelectionType.Repository,
        repository,
        state: { ...selected, isPushPullFetchInProgress: true },
      },
    }
    for (const id of ['push', 'pull', 'fetch']) {
      assert.equal(enabled(id), false)
      await assert.rejects(presenter.execute(id), /no longer available/)
    }
    state = {
      ...ready,
      selectedState: {
        type: SelectionType.Repository,
        repository,
        state: {
          ...selected,
          changesState: {
            ...selected.changesState,
            assistedCommit: {
              kind: 'preparing',
              runId: 'fixture',
              cancelRequested: false,
            },
          },
        },
      },
    }
    for (const id of [
      'push',
      'remove-repository',
      'merge-branch',
      'create-branch',
    ]) {
      assert.equal(enabled(id), false)
    }
    state = { ...ready, currentPopup: { type: PopupType.About, id: 42 } }
    assert.equal(enabled('new-repository'), false)
    assert.equal(enabled('repository'), false)
    state = { ...ready, showWelcomeFlow: true }
    assert.equal(enabled('new-repository'), false)
    state = { ...ready, selectedState: null, repositories: [] }
    assert.equal(enabled('new-repository'), true)
    assert.equal(enabled('push'), false)
    presenter.dispose()
  })

  it('emits toolkit-only actions, never DOM editing or raw state', async () => {
    const { presenter, setMenu } = await fixture()
    const actions: string[] = []
    const sub = presenter.onDidPerformAction(action => actions.push(action))
    for (const event of [
      'select-all',
      'find-text',
      'increase-active-resizable-width',
      'decrease-active-resizable-width',
    ] as const) {
      await presenter.handleEvent(event)
    }
    assert.deepEqual(actions, [
      'select-all',
      'find-text',
      'increase-active-resizable-width',
      'decrease-active-resizable-width',
    ])
    const menu = menuFromElectronMenu(
      electronMenu(buildDefaultMenuTemplate(base))
    )
    Object.assign(menu.items[0], {
      token: 'TOKEN-SENTINEL',
      callback: () => {},
    })
    setMenu(menu)
    assert.doesNotMatch(
      JSON.stringify(presenter.getState()),
      /SENTINEL|callback|oauthState|accounts/
    )
    sub.dispose()
    presenter.dispose()
  })

  it('checks the live Electron ancestor before executing an ID', () => {
    const menu = electronMenu([
      {
        id: 'root',
        label: 'Root',
        submenu: [
          { id: 'run', label: 'Run' },
          { id: 'hidden', visible: false, label: 'Hidden' },
        ],
      },
    ])
    assert.equal(executableMenuItem(menu, 'run')?.id, 'run')
    assert.equal(executableMenuItem(menu, 'root'), undefined)
    assert.equal(executableMenuItem(menu, 'hidden'), undefined)
    menu.items[0].enabled = false
    assert.equal(executableMenuItem(menu, 'run'), undefined)
  })
})
