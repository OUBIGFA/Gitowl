import { createHash } from 'crypto'
import {
  Author,
  isKnownAuthor,
  KnownAuthor,
  UnknownAuthor,
} from '../../../models/author'
import type { Account } from '../../../models/account'
import type { Repository } from '../../../models/repository'
import type { GitHubUserStore } from '../../../lib/stores/github-user-store'
import {
  CoAuthorAutocompletionProvider,
  KnownUserHit,
  UserHit,
} from '../../autocompletion/user-autocompletion-provider'
import { getLegacyStealthEmailForUser } from '../../../lib/email'

/** Original AuthorInput conversion, shared with the native co-author presenter. */
export function authorFromUserHit(user: KnownUserHit): KnownAuthor {
  return {
    kind: 'known',
    name: user.name || user.username,
    email:
      user.email && user.email.length > 0
        ? user.email
        : getLegacyStealthEmailForUser(user.username, user.endpoint),
    username: user.username,
  }
}

export type AuthorInputOwner = {
  readonly repository: Repository
  readonly account: Account | undefined
}

/** Controlled AuthorInput state for non-React hosts. Author data and API lookups
 * stay in the original store/provider; only query/lookup lifetime is local. */
export class AuthorInputController {
  private owner: AuthorInputOwner | null = null
  private revision = 0
  private query = ''
  private hits: ReadonlyArray<UserHit> = []
  private searching = false
  private disposed = false
  private wasReadOnly = false
  private readonly resolving = new Map<UnknownAuthor, object>()
  private readonly resolved = new Map<UnknownAuthor, Author>()

  public constructor(
    private readonly store: GitHubUserStore,
    private readonly currentOwner: () => AuthorInputOwner | null,
    private readonly authors: () => ReadonlyArray<Author>,
    private readonly update: (authors: ReadonlyArray<Author>) => void,
    private readonly changed: () => void,
    private readonly readOnly: () => boolean = () => false
  ) {}

  private sameOwner(
    left: AuthorInputOwner | null,
    right: AuthorInputOwner | null
  ) {
    return (
      left?.repository.hash === right?.repository.hash &&
      left?.repository.gitHubRepository?.endpoint ===
        right?.repository.gitHubRepository?.endpoint &&
      left?.account === right?.account
    )
  }

  public observe() {
    const current = this.currentOwner()
    if (!this.sameOwner(current, this.owner)) {
      this.owner = current
      this.revision++
      this.query = ''
      this.hits = []
      this.searching = false
      this.resolving.clear()
      this.resolved.clear()
    }
    const readOnly = this.readOnly()
    if (readOnly && !this.wasReadOnly) {
      // Autocompletion suggestions are not author draft entries. Starting an
      // operation revokes them, but retains pending resolutions of added users.
      this.revision++
      this.hits = []
      this.searching = false
    }
    this.wasReadOnly = readOnly
  }

  public dispose() {
    this.disposed = true
    this.revision++
    this.resolving.clear()
    this.resolved.clear()
  }

  private capture(repository: Repository) {
    this.observe()
    const owner = this.owner
    if (
      this.disposed ||
      owner === null ||
      owner.repository.hash !== repository.hash ||
      owner.repository.gitHubRepository?.endpoint !==
        repository.gitHubRepository?.endpoint ||
      repository.gitHubRepository === null ||
      this.readOnly()
    ) {
      throw new Error(
        'This author input belongs to a previous repository or account'
      )
    }
    return { owner, revision: this.revision }
  }

  private isCurrent(captured: { owner: AuthorInputOwner; revision: number }) {
    this.observe()
    return (
      !this.disposed &&
      this.revision === captured.revision &&
      this.sameOwner(this.owner, captured.owner)
    )
  }

  private provider(owner: AuthorInputOwner) {
    return new CoAuthorAutocompletionProvider(
      this.store,
      owner.repository.gitHubRepository!,
      owner.account
    )
  }

  public getState() {
    this.observe()
    this.flushResolved()
    return {
      authorQuery: this.query,
      authorSearching: this.searching,
      authorHits: this.hits.map((hit, index) => ({
        id: `${this.revision}:${index}`,
        username: hit.username,
        name: hit.kind === 'known-user' ? hit.name ?? '' : '',
        email: hit.kind === 'known-user' ? hit.email : '',
      })),
      coAuthors: this.authors().map((author, index) => ({
        id: authorInputID(author, index),
        kind: author.kind,
        username: author.username,
        name: isKnownAuthor(author) ? author.name : '',
        email: isKnownAuthor(author) ? author.email : '',
        state: isKnownAuthor(author) ? null : author.state,
      })),
    }
  }

  public async search(repository: Repository, query: string) {
    this.capture(repository)
    this.revision++
    const captured = this.capture(repository)
    this.query = query
    this.hits = []
    this.searching = true
    this.changed()
    try {
      const hits = await this.provider(captured.owner).getAutocompletionItems(
        query
      )
      if (this.isCurrent(captured)) {
        this.hits = hits.filter(
          hit =>
            hit.kind !== 'known-user' ||
            !this.authors().some(author => author.username === hit.username)
        )
      }
    } finally {
      if (this.isCurrent(captured)) {
        this.searching = false
        this.changed()
      }
    }
  }

  public async add(repository: Repository, id: string) {
    const captured = this.capture(repository)
    const hit = this.hits.find((_, index) => `${this.revision}:${index}` === id)
    if (hit === undefined) {
      throw new Error('This author suggestion is no longer available')
    }
    const author: Author =
      hit.kind === 'known-user'
        ? authorFromUserHit(hit)
        : { kind: 'unknown', username: hit.username, state: 'searching' }
    this.update([...this.authors(), author])
    this.query = ''
    this.hits = []
    this.changed()
    if (!isKnownAuthor(author)) {
      await this.resolve(author, captured.owner)
    }
  }

  public remove(repository: Repository, id: string) {
    this.capture(repository)
    const authors = this.authors()
    const index = authors.findIndex(
      (author, index) => authorInputID(author, index) === id
    )
    if (index < 0) {
      throw new Error('This co-author changed; review the current list')
    }
    this.update(authors.filter((_, current) => current !== index))
    this.changed()
  }

  /** Remounting an input resumes pending unknown users, just like AuthorInput. */
  public async resume(repository: Repository) {
    const { owner } = this.capture(repository)
    await Promise.all(
      this.authors()
        .filter(
          (author): author is UnknownAuthor =>
            !isKnownAuthor(author) && author.state === 'searching'
        )
        .map(author => this.resolve(author, owner))
    )
  }

  private async resolve(author: UnknownAuthor, owner: AuthorInputOwner) {
    if (this.resolving.has(author) || this.resolved.has(author)) {
      return
    }
    const lookup = {}
    this.resolving.set(author, lookup)
    const known = this.authors()
      .filter(isKnownAuthor)
      .find(
        candidate =>
          candidate.username?.toLowerCase() === author.username.toLowerCase()
      )
    const hit =
      known === undefined
        ? await this.provider(owner).exactMatch(author.username)
        : null
    // A newer search must not cancel already-added authors. Only page/account
    // replacement or removing that exact draft object revokes this lookup.
    this.observe()
    if (
      this.disposed ||
      !this.sameOwner(owner, this.currentOwner()) ||
      this.resolving.get(author) !== lookup ||
      !this.authors().includes(author)
    ) {
      return
    }
    this.resolving.delete(author)
    const resolved: Author =
      known ??
      (hit?.kind === 'known-user'
        ? authorFromUserHit(hit)
        : { ...author, state: 'error' })
    this.resolved.set(author, resolved)
    this.flushResolved()
  }

  /** Like AuthorInput's pending resolutions, completing HTTP must not change
   * authors while the original commit callback has made the input read-only. */
  private flushResolved() {
    if (this.disposed || this.readOnly() || this.resolved.size === 0) {
      return
    }
    const current = this.authors()
    const next = current.map(author =>
      isKnownAuthor(author) ? author : this.resolved.get(author) ?? author
    )
    this.resolved.clear()
    if (next.some((author, index) => author !== current[index])) {
      this.update(next)
      this.changed()
    }
  }
}

function authorInputID(author: Author, index: number) {
  return `${index}:${createHash('sha256')
    .update(JSON.stringify(author))
    .digest('hex')}`
}
