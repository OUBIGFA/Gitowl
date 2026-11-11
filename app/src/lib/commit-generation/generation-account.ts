import { Account, isSameAccount } from '../../models/account'
import {
  Repository,
  getRepositoryAccountIdentity,
} from '../../models/repository'
import { getAccountForCommitMessageGeneration } from '../get-account-for-repository'

/**
 * Keep Desktop's eligibility/default for an unassigned repository, but honor an
 * explicit Gitowl account binding. Signing out its account is not permission to
 * send this repository's diff through another signed-in identity.
 */
export function resolveCommitGenerationAccount(
  accounts: ReadonlyArray<Account>,
  repository: Repository
): Account | undefined {
  const account = getAccountForCommitMessageGeneration(accounts, repository)
  const assigned = getRepositoryAccountIdentity(repository)
  return assigned !== null &&
    (account === undefined || !isSameAccount(account, assigned))
    ? undefined
    : account
}
