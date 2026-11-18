import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import {
  SnapshotPushEncoder,
  NativeKernelStatePush,
} from '../../src/native-kernel/snapshot-sections'
import type { INativeKernelSnapshot } from '../../src/native-kernel/protocol'

interface IParts {
  readonly menu?: unknown
  readonly menus?: unknown
  readonly visibleFileIDs?: unknown
  readonly files?: unknown
  readonly history?: unknown
  readonly historyFiles?: unknown
  readonly diff?: unknown
  readonly ahead?: number
  readonly selected?: boolean
  readonly workspace?: boolean
}

/** Only the members the encoder reads, plus scalars it must leave alone. */
function snapshot(revision: number, parts: IParts = {}): INativeKernelSnapshot {
  return {
    protocol: 1,
    revision,
    repositories: [],
    menu: 'menu' in parts ? parts.menu : { items: [{ id: 'file' }] },
    workspace:
      parts.workspace === false
        ? undefined
        : {
            navigation: 'none',
            menus: parts.menus ?? [{ lease: 'lease-1', items: [] }],
            visibleFileIDs: parts.visibleFileIDs ?? ['a.md'],
          },
    selected:
      parts.selected === false
        ? null
        : {
            section: 'changes',
            ahead: parts.ahead ?? 0,
            files: parts.files ?? [{ id: 'a.md', path: 'a.md' }],
            history: parts.history ?? [{ sha: '1111111', summary: 'first' }],
            historyFiles: parts.historyFiles ?? [],
            diff: 'diff' in parts ? parts.diff : { kind: 'text', text: '@@' },
          },
  } as unknown as INativeKernelSnapshot
}

/**
 * The receiving contract: a section named unchanged takes the value the
 * previous push ended up with. Returns what the native UI would be given.
 */
function receive(
  push: NativeKernelStatePush,
  previous: Record<string, any> | undefined
): Record<string, any> {
  const { unchanged, ...wire } = JSON.parse(JSON.stringify(push))
  if (unchanged === undefined) {
    return wire
  }
  assert.equal(unchanged.base, previous?.revision, 'push base')
  for (const section of unchanged.sections as string[]) {
    const [parent, name] = section.split('.')
    if (name === undefined) {
      wire[parent] = previous![parent]
    } else {
      wire[parent][name] = previous![parent][name]
    }
  }
  return wire
}

describe('native kernel snapshot push sections', () => {
  it('sends the first push complete', () => {
    const first = snapshot(1)
    assert.equal(new SnapshotPushEncoder().encode(first), first)
  })

  it('omits only the sections that the previous push delivered', () => {
    const encoder = new SnapshotPushEncoder()
    encoder.encode(snapshot(1))
    const push = encoder.encode(
      snapshot(2, { files: [{ id: 'b.md', path: 'b.md' }], ahead: 3 })
    )

    assert.deepEqual(push.unchanged, {
      base: 1,
      sections: [
        'menu',
        'workspace.menus',
        'workspace.visibleFileIDs',
        'selected.history',
        'selected.historyFiles',
        'selected.diff',
      ],
    })
    const wire = JSON.parse(JSON.stringify(push))
    assert.equal('menu' in wire, false)
    assert.deepEqual(Object.keys(wire.workspace), ['navigation'])
    assert.deepEqual(wire.selected, {
      section: 'changes',
      ahead: 3,
      files: [{ id: 'b.md', path: 'b.md' }],
    })
  })

  it('does not alter the snapshot a command response also carries', () => {
    const encoder = new SnapshotPushEncoder()
    encoder.encode(snapshot(1))
    const second = snapshot(2)
    const complete = JSON.stringify(second)
    encoder.encode(second)
    assert.equal(JSON.stringify(second), complete)
  })

  it('reconstructs every push across changing, vanishing and returning sections', () => {
    const sequence = [
      snapshot(1),
      snapshot(2, { ahead: 1 }),
      snapshot(3, { history: [{ sha: '2222222', summary: 'second' }] }),
      // A repeated null diff is an unchanged section like any other.
      snapshot(4, { diff: null }),
      snapshot(5, { diff: null, menus: [] }),
      // No repository: the selected sections have no value to build on next.
      snapshot(6, { selected: false, workspace: false, menu: undefined }),
      snapshot(7),
      snapshot(8, { visibleFileIDs: [] }),
      snapshot(9, { visibleFileIDs: [] }),
    ]
    const encoder = new SnapshotPushEncoder()
    let previous: Record<string, any> | undefined
    for (const complete of sequence) {
      const push = encoder.encode(complete)
      previous = receive(push, previous)
      assert.deepEqual(
        previous,
        JSON.parse(JSON.stringify(complete)),
        `revision ${complete.revision}`
      )
    }
  })

  it('never builds on a section the previous push did not contain', () => {
    const encoder = new SnapshotPushEncoder()
    encoder.encode(snapshot(1))
    encoder.encode(snapshot(2, { selected: false }))
    // Same content as revision 1, but revision 2 had no selection to copy from.
    const push = encoder.encode(snapshot(3))
    assert.equal(
      push.unchanged?.sections.some(name => name.startsWith('selected.')),
      false
    )
    assert.deepEqual(push.selected, snapshot(3).selected)
  })

  it('treats loaded history as unchanged while only its timer hint moves', () => {
    const commit = {
      sha: '1111111',
      summary: 'first',
      dateLabels: { en: 'now' },
    }
    const encoder = new SnapshotPushEncoder()
    encoder.encode(
      snapshot(1, { history: [{ ...commit, dateRefreshAfter: 60000 }] })
    )
    const ticking = encoder.encode(
      snapshot(2, { history: [{ ...commit, dateRefreshAfter: 59000 }] })
    )
    assert.ok(ticking.unchanged?.sections.includes('selected.history'))

    // A label that actually changed is content and is sent again.
    const relabeled = encoder.encode(
      snapshot(3, {
        history: [{ ...commit, dateLabels: { en: '1 minute ago' } }],
      })
    )
    assert.equal(
      relabeled.unchanged?.sections.includes('selected.history') ?? false,
      false
    )
  })

  it('keeps a repeated progress push small beside a heavy snapshot', () => {
    const heavy = {
      menus: Array.from({ length: 300 }, (_, i) => ({
        lease: `lease-${i}`,
        items: Array.from({ length: 12 }, (_, j) => ({
          labelKey: `item-${j}`,
        })),
      })),
      history: Array.from({ length: 100 }, (_, i) => ({
        sha: String(i).padStart(40, '0'),
        summary: `change ${i}`,
      })),
    }
    const encoder = new SnapshotPushEncoder()
    const first = JSON.stringify(encoder.encode(snapshot(1, heavy))).length
    const repeat = JSON.stringify(
      encoder.encode(snapshot(2, { ...heavy, ahead: 1 }))
    ).length
    assert.ok(
      repeat * 20 < first,
      `repeat push ${repeat} B against first push ${first} B`
    )
  })
})
