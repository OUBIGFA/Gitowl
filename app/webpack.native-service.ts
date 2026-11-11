import * as path from 'path'
import webpack from 'webpack'
import merge from 'webpack-merge'
import { getKoffiWebpackConfig } from '../script/webpack-koffi'
import { getReplacements } from './app-info'

// A Node service, not an Electron renderer. Loading it in plain Node is part of
// the integration tests so DOM/React dependencies cannot creep back in unnoticed.
const config: webpack.Configuration = {
  mode: 'production',
  target: 'node',
  entry: path.resolve(__dirname, 'src/native-service/main.ts'),
  output: {
    path: path.resolve(__dirname, '../out/native-service'),
    filename: 'service.js',
  },
  devtool: 'source-map',
  optimization: { minimize: false, emitOnErrors: false },
  resolve: { extensions: ['.ts', '.tsx', '.js'], aliasFields: [] },
  node: { __dirname: false, __filename: false },
  module: {
    rules: [
      {
        test: /\.tsx?$/,
        include: path.resolve(__dirname, 'src'),
        loader: 'ts-loader',
      },
      {
        test: /\.node$/,
        loader: 'awesome-node-loader',
        options: { name: '[name].[ext]' },
      },
    ],
  },
  plugins: [
    new webpack.DefinePlugin({
      ...getReplacements(),
      __APP_NAME__: JSON.stringify('Gitowl'),
      __APP_VERSION__: JSON.stringify('0.0.1'),
      __PROCESS_KIND__: JSON.stringify('native-service'),
      __UPDATES_URL__: JSON.stringify(''),
    }),
  ],
}

export default merge(
  config,
  getKoffiWebpackConfig(
    path.resolve(__dirname, 'node_modules'),
    process.platform,
    process.arch
  )
)
