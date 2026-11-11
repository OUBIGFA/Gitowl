# Gitowl

Gitowl is a public derivative of [GitHub Desktop](https://github.com/desktop/desktop). The migration replaces the frontend with MyGo native UI while directly reusing the original Desktop application kernel, stores, dispatcher, credential lifecycle, Git operations, platform integrations, and recovery workflows. Feature and behavior parity is mandatory; a smaller independently coordinated Git backend is not the product.

## Current repository state

- The default branch is `main`.
- The upstream source revision is `a95e13a7429f4e7f9e45d7b38a68bc1a3eb972f7`.
- The MyGo launcher now connects only to the original Desktop Electron kernel. Both frontends use the shared [Desktop runtime assembly](app/src/ui/app-runtime.ts), and [presentation commands](app/src/native-kernel/desktop-presenter.ts) forward to its original Dispatcher. The native pipe and a bounded popup/recovery adapter are implemented and exercised against real Git; the full native frontend and all interaction contracts remain incomplete.
- The fixed Windows installer is not accepted or delivered yet. `npm run build:native` must not be used to label the existing subset prototype as a full replacement; see [the build and acceptance boundaries](references/native-installer.md).
- Same-source fork findings and implementation tradeoffs are recorded in [the source evaluation](third_party/fork-evaluation.md). The [feature declaration inventory](references/upstream-features.json) tracks 315 menu, popup, banner, foldout, preferences, repository-tab, IPC, deep-link, and recovery declarations; it does not certify runtime feature parity.
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
npm run check:features
npm run test:repo
```

Use the existing upstream commands for application linting, tests, development builds, production builds, and packaging. Distribution output now goes to ignored `output/distribution/`; compilation output stays in `out/`. The build only clears its distribution directory, preserving the supplied `dist/logo.png`. The canonical Gitowl logo source is [app/static/logos/gitowl/logo.png](app/static/logos/gitowl/logo.png); application icons and runtime branding have not yet been migrated.

## Repository boundaries

- Publishable source, tests, lock files, build definitions, and license records belong in Git.
- Small public fixtures belong in `references/` or the relevant test fixture directory.
- Reusable local downloads, toolchains, upstream snapshots, MyGo working copies, private inputs, and caches belong in ignored `.local/`.
- The root `docs/` directory, agent instructions, `.agents/`, `output/`, and `_temp/` are local-only and never enter the Git index.
- Third-party sources and licensing decisions are recorded in [`third_party/README.md`](third_party/README.md).

## License

The upstream application is distributed under the [MIT License](LICENSE). Third-party code and assets retain their own licenses. GitHub trademarks and branding remain subject to the upstream notices and applicable trademark rules.
