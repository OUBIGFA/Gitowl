import { Server } from 'http'
import { AddressInfo } from 'net'

// Fetch rejects these service ports even on loopback. Windows can include
// them in a customized ephemeral range, so listen(0) alone is insufficient.
// https://fetch.spec.whatwg.org/#port-blocking
const fetchBlockedPorts = new Set([
  0, 1, 7, 9, 11, 13, 15, 17, 19, 20, 21, 22, 23, 25, 37, 42, 43, 53, 69, 77,
  79, 87, 95, 101, 102, 103, 104, 109, 110, 111, 113, 115, 117, 119, 123, 135,
  137, 139, 143, 161, 179, 389, 427, 465, 512, 513, 514, 515, 526, 530, 531,
  532, 540, 548, 554, 556, 563, 587, 601, 636, 989, 990, 993, 995, 1719, 1720,
  1723, 2049, 3659, 4045, 4190, 5060, 5061, 6000, 6566, 6665, 6666, 6667, 6668,
  6669, 6679, 6697, 10080,
])

/** Bind a real test server to an OS-assigned port usable by HTTP SDKs. */
export async function listenOnLoopback(server: Server): Promise<void> {
  while (true) {
    await new Promise<void>((resolve, reject) => {
      server.once('error', reject)
      server.listen(0, '127.0.0.1', () => {
        server.removeListener('error', reject)
        resolve()
      })
    })
    if (!fetchBlockedPorts.has((server.address() as AddressInfo).port)) {
      return
    }
    // Release only this listener and ask the OS again. Requests are not retried,
    // and real bind/close errors still fail the test instead of being swallowed.
    await new Promise<void>((resolve, reject) =>
      server.close(error => (error ? reject(error) : resolve()))
    )
  }
}
