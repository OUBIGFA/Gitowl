import assert from 'node:assert/strict'
import { it, TestContext } from 'node:test'
import { mkdir, writeFile } from 'fs/promises'
import { dirname, join } from 'path'
import {
  CopilotClient,
  CopilotRequestHandler,
  RuntimeConnection,
} from '@github/copilot-sdk'
import {
  getCopilotBaseDirectory,
  getCopilotRuntimePath,
} from '../../src/lib/copilot-runtime'
import {
  createCopilotInMemorySessionFsProvider,
  getCopilotInMemorySessionFsConfig,
} from '../../src/lib/copilot-in-memory-session-fs-provider'
import { pathExists } from '../../src/lib/path-exists'
import { createTempDirectory } from '../helpers/temp'

/** Answers the session locally: no account, network or paid request. */
class LocalModel extends CopilotRequestHandler {
  public async sendRequest(request: Request) {
    const body = await request.json()
    const message = {
      id: 'local',
      created: 0,
      model: 'local-model',
      usage: { prompt_tokens: 1, completion_tokens: 1, total_tokens: 2 },
    }
    const content = '{"title":"Update notes","description":""}'
    if (!body.stream) {
      return Response.json({
        ...message,
        object: 'chat.completion',
        choices: [
          {
            index: 0,
            message: { role: 'assistant', content },
            finish_reason: 'stop',
          },
        ],
      })
    }
    const chunk = (delta: object, finish: string | null) =>
      `data: ${JSON.stringify({
        ...message,
        object: 'chat.completion.chunk',
        choices: [{ index: 0, delta, finish_reason: finish }],
      })}\n\n`
    return new Response(
      chunk({ role: 'assistant', content }, null) +
        chunk({}, 'stop') +
        'data: [DONE]\n\n',
      { headers: { 'Content-Type': 'text/event-stream' } }
    )
  }

  public async openWebSocket(): Promise<never> {
    throw new Error('This test makes no remote connection')
  }
}

/**
 * Run one session with Desktop's session policy in a profile whose Copilot CLI
 * home has a user-level hook, and report whether that hook ran.
 */
async function userHookRan(t: TestContext, ownedHome: boolean) {
  const profile = await createTempDirectory(t)
  const marker = join(profile, 'hook-ran')
  await mkdir(join(profile, '.copilot', 'hooks'), { recursive: true })
  // A plain file write: the hook itself starts no other console program.
  await writeFile(
    join(profile, '.copilot', 'hooks', 'user.json'),
    JSON.stringify({
      version: 1,
      hooks: {
        SessionStart: [
          {
            type: 'command',
            bash: `echo ran > '${marker}'`,
            powershell: `Set-Content -LiteralPath '${marker}' -Value ran`,
            timeoutSec: 20,
          },
        ],
      },
    })
  )
  const runtimeRoot = dirname(
    require.resolve(
      `@github/copilot-sdk-${process.platform}-${process.arch}/package.json`
    )
  )
  const client = new CopilotClient({
    connection: RuntimeConnection.forStdio({
      path: getCopilotRuntimePath(runtimeRoot),
    }),
    baseDirectory: ownedHome
      ? getCopilotBaseDirectory(join(profile, 'application-data'))
      : undefined,
    useLoggedInUser: false,
    workingDirectory: profile,
    logLevel: 'none',
    env: { ...process.env, HOME: profile, USERPROFILE: profile },
    onListModels: () => [],
    sessionFs: getCopilotInMemorySessionFsConfig(
      profile,
      process.platform === 'win32' ? 'windows' : 'posix'
    ),
    requestHandler: new LocalModel(),
  })
  try {
    // The options CopilotStore.createSession applies to every session.
    const session = await client.createSession({
      model: 'local-model',
      provider: {
        type: 'openai',
        baseUrl: 'http://127.0.0.1:1',
        wireApi: 'completions',
      },
      enableFileHooks: false,
      enableConfigDiscovery: false,
      enableSkills: false,
      enableSessionStore: false,
      availableTools: [],
      createSessionFsProvider: createCopilotInMemorySessionFsProvider,
      onPermissionRequest: async () => ({ kind: 'reject' }),
    })
    await session.sendAndWait({ prompt: 'Describe the change.' }, 60000)
    await session.disconnect()
    await client.stop()
  } finally {
    await client.forceStop()
  }
  return pathExists(marker)
}

it('runs no user-level Copilot CLI hook in a session with the application-owned home', async t => {
  // The control proves the fixture: repository hook options alone do not stop
  // a hook installed in the Copilot CLI's own home.
  assert.equal(await userHookRan(t, false), true)
  assert.equal(await userHookRan(t, true), false)
})
