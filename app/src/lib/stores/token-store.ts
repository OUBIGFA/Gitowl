import * as keytar from 'keytar'

/** Product identity is adapted once at the secure-storage platform boundary. */
function credentialKey(key: string): string {
  if (process.env.GITOWL_NATIVE_CORE !== '1') {
    return key
  }
  const namespace = process.env.GITOWL_CREDENTIAL_NAMESPACE
  if (!namespace) {
    // Never fall back to the installed upstream application's credentials when
    // a malformed native launch skipped its identity bootstrap.
    throw new Error('Missing Gitowl credential namespace')
  }
  return `${namespace} :: ${key}`
}

function setItem(key: string, login: string, value: string) {
  return keytar.setPassword(credentialKey(key), login, value)
}

function getItem(key: string, login: string) {
  return keytar.getPassword(credentialKey(key), login)
}

function deleteItem(key: string, login: string) {
  return keytar.deletePassword(credentialKey(key), login)
}

export const TokenStore = {
  setItem,
  getItem,
  deleteItem,
}
