# Contributing to Gitowl

Thanks for helping improve Gitowl. It is a derivative of GitHub Desktop, so changes should preserve upstream feature entry points and document any intentional behavior difference.

## Before changing code

- Work on `main` unless a separate branch has been explicitly authorized.
- Keep source, tests, lock files, build definitions, and license records in the index.
- Keep root `docs/`, agent instructions, `.agents/`, generated output, downloads, caches, and `_temp/` local.
- Record third-party sources and licenses in `third_party/README.md` before adding them to a build.

## Checks

Run the repository checks from the root:

```bash
npm run check:repo
npm run test:repo
```

Run the affected upstream application tests and lint commands before submitting a code change. Repository checks do not replace application build or runtime acceptance.

## Pull requests

Describe the user-visible behavior, affected feature entry points, tests, and any license or source changes. Keep commits focused and do not force-push shared history.
