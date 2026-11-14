import assert from 'node:assert/strict'
import { it } from 'node:test'
import { getCopilotRuntimeLaunch } from '../../src/lib/copilot-runtime'

it('routes the Windows SDK through the native host while keeping the original runtime argument intact', () => {
  const runtime = 'C:\\Program Files\\Gitowl\\copilot\\copilot-runtime.exe'
  assert.deepEqual(
    getCopilotRuntimeLaunch(
      runtime,
      'C:\\Program Files\\Gitowl\\Gitowl.exe',
      'win32'
    ),
    {
      path: 'C:\\Program Files\\Gitowl\\Gitowl.exe',
      args: ['--gitowl-copilot-runtime', runtime],
    }
  )
})

it('retains the original SDK launch outside the native Windows host', () => {
  assert.deepEqual(
    getCopilotRuntimeLaunch('/runtime/copilot', undefined, 'win32'),
    {
      path: '/runtime/copilot',
    }
  )
  assert.deepEqual(
    getCopilotRuntimeLaunch('/runtime/copilot', '/native/Gitowl', 'darwin'),
    {
      path: '/runtime/copilot',
    }
  )
})
