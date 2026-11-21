import * as path from 'path'
import webpack from 'webpack'
import merge from 'webpack-merge'
import { getReplacements } from './app-info'
import { getKoffiWebpackConfig } from '../script/webpack-koffi'

/** Full business runtime on plain Node. External OS/codec/storage packages keep
 * their published package layouts and verified native binaries at packaging.
 */
const config: webpack.Configuration = {
  mode: 'production',
  target: 'node',
  cache: {
    type: 'filesystem',
    cacheDirectory: path.resolve(
      __dirname,
      '../.local/webpack-cache/node-kernel'
    ),
  },
  entry: {
    main: path.resolve(__dirname, 'src/native-kernel/node-main.ts'),
    migration: path.resolve(__dirname, 'src/native-kernel/migration-main.ts'),
    cli: path.resolve(__dirname, 'src/cli/main.ts'),
    'install-cli': path.resolve(__dirname, 'src/native-kernel/install-cli.ts'),
    'image-worker': path.resolve(
      __dirname,
      'src/native-kernel/image-worker.ts'
    ),
  },
  output: {
    path: path.resolve(__dirname, '../out/native-kernel-node'),
    filename: '[name].js',
  },
  devtool: 'source-map',
  optimization: {
    minimize: false,
    emitOnErrors: false,
    splitChunks: { chunks: 'all' },
  },
  externals: [
    ({ request }, done) => {
      if (
        request &&
        /^(indexeddbshim|@imagemagick\/magick-wasm|upng-js|jsdom|undici)(\/|$)/.test(
          request
        )
      ) {
        done(null, `commonjs ${request}`)
      } else if (request === 'electron') {
        done(new Error('The production Node kernel still imports Electron'))
      } else {done()}
    },
  ],
  resolve: {
    extensions: ['.ts', '.tsx', '.js'],
    aliasFields: [],
    alias: {
      'desktop-trampoline$': path.resolve(
        __dirname,
        '../vendor/desktop-trampoline/index.ts'
      ),
    },
  },
  node: { __dirname: false, __filename: false },
  module: {
    rules: [
      {
        test: /\.tsx?$/,
        include: [
          path.resolve(__dirname, 'src'),
          path.resolve(__dirname, '../vendor/desktop-trampoline'),
          path.resolve(__dirname, 'node_modules/desktop-trampoline'),
        ],
        loader: 'ts-loader',
        options: { allowTsInNodeModules: true },
      },
      {
        test: /\.node$/,
        loader: 'awesome-node-loader',
        options: { name: '[name].[ext]' },
      },
      { test: /\.cmd$/, type: 'asset/resource' },
    ],
  },
  plugins: [
    {
      apply(compiler) {
        compiler.hooks.thisCompilation.tap('NodeRuntimeAssets', compilation => {
          compilation.hooks.processAssets.tap(
            {
              name: 'NodeRuntimeAssets',
              stage: webpack.Compilation.PROCESS_ASSETS_STAGE_REPORT,
            },
            () => {
              const sourceModules = [...compilation.modules]
                .flatMap(module => {
                  const resource = (module as webpack.NormalModule).resource
                  return resource
                    ? [
                        path
                          .relative(path.resolve(__dirname, '..'), resource)
                          .replaceAll('\\', '/'),
                      ]
                    : []
                })
                .sort()
              compilation.emitAsset(
                'node-runtime-sources.json',
                new webpack.sources.RawSource(
                  JSON.stringify(sourceModules) + '\n'
                )
              )
              const assets = compilation
                .getAssets()
                .map(asset => asset.name)
                .filter(name => !name.endsWith('.map'))
              compilation.emitAsset(
                'node-runtime-assets.json',
                new webpack.sources.RawSource(
                  JSON.stringify(assets.sort()) + '\n'
                )
              )
            }
          )
        })
      },
    },
    new webpack.DefinePlugin({
      ...getReplacements(),
      __APP_NAME__: JSON.stringify('Gitowl'),
      __APP_VERSION__: JSON.stringify('0.1.0'),
      __PROCESS_KIND__: JSON.stringify('native-service'),
      __UPDATES_URL__: JSON.stringify(''),
    }),
  ],
}
// eslint-disable-next-line no-restricted-syntax
export default merge(
  config,
  getKoffiWebpackConfig(
    path.resolve(__dirname, 'node_modules'),
    process.platform,
    process.arch
  )
)
