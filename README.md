# Gitowl

Gitowl is a public derivative of [GitHub Desktop](https://github.com/desktop/desktop), built as the foundation for a MyGo based desktop client. The repository starts from the upstream `development` baseline and keeps the upstream application, test suite, build entry points, and feature entry points available for comparison while the UI migration proceeds.

## Current repository state

- The default branch is `main`.
- The upstream source revision is `a95e13a7429f4e7f9e45d7b38a68bc1a3eb972f7`.
- The MyGo UI migration is planned and must be verified feature by feature.
- A passing repository hygiene check does not mean that the desktop application has passed a build or runtime acceptance test.

## Development

Install the pinned Node and npm versions from the version files, then initialize the submodules and install dependencies:

```bash
git submodule update --init --recursive
npm ci
```

Repository checks use the staged Git snapshot:

```bash
npm run check:repo
npm run test:repo
```

Use the existing upstream commands for application linting, tests, development builds, production builds, and packaging. Keep generated output in ignored `output/` or `out/` directories.

## Repository boundaries

- Publishable source, tests, lock files, build definitions, and license records belong in Git.
- Small public fixtures belong in `references/` or the relevant test fixture directory.
- Reusable local downloads, toolchains, upstream snapshots, MyGo working copies, private inputs, and caches belong in ignored `.local/`.
- The root `docs/` directory, agent instructions, `.agents/`, `output/`, and `_temp/` are local-only and never enter the Git index.
- Third-party sources and licensing decisions are recorded in [`third_party/README.md`](third_party/README.md).

## License

The upstream application is distributed under the [MIT License](LICENSE). Third-party code and assets retain their own licenses. GitHub trademarks and branding remain subject to the upstream notices and applicable trademark rules.
