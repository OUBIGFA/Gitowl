import assert from 'node:assert/strict'
import { it } from 'node:test'
import { mkdirSync, writeFileSync, readFileSync } from 'node:fs'
import { join } from 'node:path'
import fixtures from '../../../references/native-image-codecs.json'
import { startNodeFixture } from './node-client-fixture'

it(
  'runs real status, selected commit, push/fetch, history, codec worker and restart persistence through the complete Node backend',
  { timeout: 120000 },
  async () => {
    let client = startNodeFixture()
    const repo = join(client.profile, 'project'),
      remote = join(client.profile, 'remote.git')
    mkdirSync(repo)
    client.git(repo, 'init', '--initial-branch=main')
    client.git(repo, 'config', 'user.name', 'Native Backend Test')
    client.git(repo, 'config', 'user.email', 'backend@example.invalid')
    client.git(repo, 'config', 'commit.gpgsign', 'false')
    writeFileSync(join(repo, 'selected.txt'), 'before\n')
    writeFileSync(join(repo, 'retained.txt'), 'before\n')
    writeFileSync(
      join(repo, 'animation.png'),
      Buffer.from(fixtures.apng.previous, 'base64')
    )
    client.git(repo, 'add', '.')
    client.git(repo, 'commit', '-m', 'baseline')
    client.git(repo, 'clone', '--bare', repo, remote)
    client.git(repo, 'remote', 'add', 'origin', remote)
    client.git(repo, 'fetch', 'origin')
    client.git(repo, 'branch', '--set-upstream-to=origin/main', 'main')
    writeFileSync(join(repo, 'selected.txt'), 'after\n')
    writeFileSync(join(repo, 'retained.txt'), 'keep uncommitted\n')
    writeFileSync(
      join(repo, 'animation.png'),
      Buffer.from(fixtures.apng.contents, 'base64')
    )
    await client.ready
    try {
      const added = await client.command({
        name: 'repositories.add',
        paths: [repo],
      })
      const repository = added.addedRepositories[0]
      const secondRepo = join(client.profile, 'second-project')
      client.git(client.profile, 'init', '--initial-branch=main', secondRepo)
      const second = await client.command({
        name: 'repositories.add',
        paths: [secondRepo],
      })
      assert.equal(
        second.snapshot.repositories.length,
        2,
        JSON.stringify(second) + client.diagnostic()
      )
      await client.command({ name: 'repository.select', repository })
      const refreshed = await client.command({
        name: 'repository.refresh',
        repository,
      })
      assert.equal(refreshed.snapshot.selected.files.length, 3)
      const selected = refreshed.snapshot.selected.files.find(
        (file: any) => file.path === 'selected.txt'
      )
      await client.command({
        name: 'repository.files.includeAll',
        repository,
        included: false,
      })
      await client.command({
        name: 'repository.files.include',
        repository,
        fileIDs: [selected.id],
        included: true,
      })
      const committed = await client.command({
        name: 'repository.commit',
        repository,
        context: { summary: 'selected only', description: null },
      })
      assert.equal(committed.committed, true)
      assert.equal(client.git(repo, 'show', 'HEAD:selected.txt'), 'after')
      assert.equal(client.git(repo, 'show', 'HEAD:retained.txt'), 'before')
      assert.equal(
        readFileSync(join(repo, 'retained.txt'), 'utf8'),
        'keep uncommitted\n'
      )
      await client.command({ name: 'repository.push', repository })
      await client.command({ name: 'repository.fetch', repository })
      assert.equal(
        client.git(repo, 'rev-parse', 'HEAD'),
        client.git(remote, 'rev-parse', 'main')
      )
      const history = await client.command({
        name: 'repository.section',
        repository,
        section: 'history',
      })
      assert.ok(
        history.snapshot.selected.history.some(
          (commit: any) => commit.summary === 'selected only'
        )
      )
      await client.command({
        name: 'repository.section',
        repository,
        section: 'changes',
      })
      const changes = await client.command({
        name: 'repository.refreshChanges',
        repository,
      })
      const image = changes.snapshot.selected.files.find(
        (file: any) => file.path === 'animation.png'
      )
      await client.command({
        name: 'repository.files.select',
        repository,
        fileIDs: [image.id],
      })
      const viewing = await client.waitFor(
        snapshot =>
          snapshot.selected?.diff?.kind === 'image' &&
          snapshot.selected.diff.current?.mediaID
      )
      const mediaID = viewing.selected.diff.current.mediaID
      const frame = await client.command({
        name: 'media.frame.read',
        mediaID,
        frame: 1,
        offset: 0,
      })
      assert.equal(frame.mediaChunk.frames, 2)
      assert.equal(frame.mediaChunk.duration, 110000)
      assert.equal(Buffer.from(frame.mediaChunk.contents, 'base64')[2], 255)
      const profile = client.profile
      await client.close()
      client = startNodeFixture(profile)
      await client.ready
      const restarted = await client.command({ name: 'state' })
      assert.ok(
        restarted.snapshot.repositories.some(
          (entry: any) => entry.path === repo
        )
      )
    } finally {
      await client.close()
    }
  }
)
