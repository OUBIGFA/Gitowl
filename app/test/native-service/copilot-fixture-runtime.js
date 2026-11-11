// Controlled SDK protocol fixture, NOT an authenticated Copilot runtime. The
// separate packaged-runtime tests establish actual connectivity/auth errors.
// Use the SDK's locked JSON-RPC transport library, not approximate framing/HTTP.
const {
  createMessageConnection,
  StreamMessageReader,
  StreamMessageWriter,
  ResponseError,
} = require('vscode-jsonrpc/node')
const { appendFileSync } = require('fs')

const trace = process.env.COPILOT_FIXTURE_TRACE
const scenario = process.env.COPILOT_FIXTURE_SCENARIO
const connection = createMessageConnection(
  new StreamMessageReader(process.stdin),
  new StreamMessageWriter(process.stdout)
)
const sessions = new Map()
const permissions = new Map()
let eventId = 0
const accountIds = ['opaque:fixture/alpha+?=', 'opaque:fixture/bravo+?=']
const flowId = 'opaque:fixture/login+?=/'
const rawResponse = '{"title":"Fixture commit","description":""}'
const record = (method, params) =>
  appendFileSync(trace, JSON.stringify({ method, params }) + '\n')
const account = (selectionId, active = true) => ({
  selectionId,
  host: 'https://github.com',
  login: selectionId === accountIds[0] ? 'fixture-alpha' : 'fixture-bravo',
  kind: 'githubDotCom',
  active,
  token: 'FIXTURE SECRET MUST NOT LEAVE GATEWAY',
  authInfo: { token: 'FIXTURE SECRET MUST NOT LEAVE GATEWAY' },
})
function event(sessionId, type, data) {
  const value = {
    id: `fixture-event-${++eventId}`,
    timestamp: new Date().toISOString(),
    parentId: null,
    type,
    data,
  }
  connection.sendNotification('session.event', { sessionId, event: value })
  return value
}

connection.onRequest(async (method, params, token) => {
  record(method, params)
  const session = sessions.get(params?.sessionId)
  switch (method) {
    case 'connect':
      return { protocolVersion: 3 }
    case 'sessionFs.setProvider':
    case 'session.options.update':
      return {}
    case 'session.create': {
      sessions.set(params.sessionId, {
        accountId: accountIds[0],
        modelId: params.model,
        providerId: 'fixture-copilot',
      })
      if (scenario === 'late-create' && params.model) {
        await new Promise(resolve => setTimeout(resolve, 100))
      }
      return { sessionId: params.sessionId }
    }
    case 'session.accounts.enumerate':
      return {
        kind: 'accounts',
        items: accountIds.map(id => account(id, id === session.accountId)),
      }
    case 'session.accounts.get':
      switch (params.query.kind) {
        case 'status':
          return {
            kind: 'status',
            status: {
              isAuthenticated: true,
              accountCount: 2,
              token: 'FIXTURE SECRET MUST NOT LEAVE GATEWAY',
            },
          }
        case 'activeAccount':
          return {
            kind: 'activeAccount',
            account: account(
              scenario === 'account-mismatch' && session.modelId
                ? accountIds[1]
                : session.accountId
            ),
            authInfo: { token: 'FIXTURE SECRET MUST NOT LEAVE GATEWAY' },
          }
        case 'lastErrors':
          return {
            kind: 'lastErrors',
            errors: [
              {
                message: 'fixture validation',
                token: 'FIXTURE SECRET MUST NOT LEAVE GATEWAY',
              },
            ],
          }
      }
      break
    case 'session.accounts.set':
      if (!accountIds.includes(params.command.selectionId)) {
        throw new ResponseError(
          -32603,
          'Selected authentication is no longer available'
        )
      }
      if (params.command.kind === 'switchActive') {
        session.accountId = params.command.selectionId
        return { ok: scenario !== 'account-refused' }
      }
      return { ok: true, moreUsers: true }
    case 'session.accounts.login.begin':
      return {
        flowId,
        step: {
          kind: 'open-url',
          url: 'https://github.com/login/oauth/authorize?state=fixture',
          token: 'FIXTURE SECRET MUST NOT LEAVE GATEWAY',
        },
      }
    case 'session.accounts.login.cancel':
      return null
    case 'session.accounts.login.advance':
      throw new ResponseError(-32603, 'Unknown or completed login flow')
    case 'models.list':
      if (!accountIds.includes(params.selectionId)) {
        throw new ResponseError(
          -32603,
          'Selected authentication is no longer available'
        )
      }
      return {
        models: [
          {
            id: 'fixture-model',
            name: 'Fixture model',
            capabilities: {},
            provider: { id: 'fixture-copilot', kind: 'copilot' },
          },
        ],
      }
    case 'session.model.switchTo':
      session.modelId =
        scenario === 'model-fallback' ? 'another-model' : params.modelId
      session.providerId = params.providerId
      return { modelId: session.modelId }
    case 'session.model.getCurrent':
      return { modelId: session.modelId, providerId: session.providerId }
    case 'session.sendAndWait':
      if (scenario === 'pending') {
        return new Promise(resolve => {
          token.onCancellationRequested(() => {
            record('fixture.request-cancelled', { sessionId: params.sessionId })
            resolve({ aborted: true })
          })
        })
      }
      if (scenario === 'billing') {
        event(params.sessionId, 'session.error', {
          message: '402 fixture quota exhausted (Request ID: fixture)',
          statusCode: 402,
          errorCode: 'quota_exceeded',
          errorType: 'billing',
        })
        throw new ResponseError(-32603, 'fixture raw billing transport error')
      }
      if (scenario === 'empty') {
        return { aborted: false }
      }
      if (scenario === 'permission') {
        const requestId = `fixture-permission-${params.sessionId}`
        const decision = new Promise(resolve =>
          permissions.set(requestId, resolve)
        )
        event(params.sessionId, 'permission.requested', {
          requestId,
          permissionRequest: { kind: 'shell', commands: ['echo forbidden'] },
        })
        await decision
      }
      return {
        aborted: false,
        response: event(params.sessionId, 'assistant.message', {
          content: rawResponse,
          messageId: 'fixture-message',
        }),
      }
    case 'session.permissions.handlePendingPermissionRequest':
      permissions.get(params.requestId)?.(params.result)
      permissions.delete(params.requestId)
      return {}
    case 'session.detach':
      sessions.delete(params.sessionId)
      return { success: true }
  }
  throw new ResponseError(-32601, `Unimplemented fixture method ${method}`)
})
connection.listen()
