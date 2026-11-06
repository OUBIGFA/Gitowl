import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'fs'
import { join, relative, resolve, sep } from 'path'
import {
  getDistRoot,
  getDistPath,
  getOSXZipPath,
  getWindowsInstallerPath,
  getWindowsStandalonePath,
  getWindowsFullNugetPackagePath,
  getWindowsDeltaNugetPackagePath,
} from './dist-info'

describe('distribution output boundary', () => {
  const projectRoot = resolve(__dirname, '..')

  it('keeps package, installer and update outputs inside generated output', () => {
    const root = getDistRoot()
    assert.equal(relative(projectRoot, root), join('output', 'distribution'))
    for (const path of [
      getDistPath(),
      getOSXZipPath(),
      getWindowsInstallerPath(),
      getWindowsStandalonePath(),
      getWindowsFullNugetPackagePath(),
      getWindowsDeltaNugetPackagePath(),
    ]) {
      assert.ok(path.startsWith(root + sep), path)
    }
  })

  it('preserves logo inputs when the build clears its distribution directory', () => {
    const scratch = join(projectRoot, '_temp', 'distribution-boundary')
    mkdirSync(scratch, { recursive: true })
    const fixture = mkdtempSync(join(scratch, 'case-'))
    try {
      // Relocate the actual configured output into a disposable repository.
      // The build uses this same recursive cleanup before packaging.
      const output = join(fixture, relative(projectRoot, getDistRoot()))
      const logo = join(fixture, 'dist', 'logo.png')
      const source = join(
        fixture,
        'app',
        'static',
        'logos',
        'gitowl',
        'logo.png'
      )
      for (const path of [logo, source]) {
        mkdirSync(resolve(path, '..'), { recursive: true })
        writeFileSync(path, 'original-logo')
      }
      mkdirSync(output, { recursive: true })
      writeFileSync(join(output, 'old-build'), 'generated')
      rmSync(output, { recursive: true, force: true })
      assert.equal(readFileSync(logo, 'utf8'), 'original-logo')
      assert.equal(readFileSync(source, 'utf8'), 'original-logo')
    } finally {
      rmSync(fixture, { recursive: true, force: true })
    }
  })
})
