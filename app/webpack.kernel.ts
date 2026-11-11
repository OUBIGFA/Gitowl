import * as path from 'path'
import * as common from './webpack.common'
import merge from 'webpack-merge'
import type webpack from 'webpack'

// A bounded development build of the same upstream main/kernel sources. The
// original frontend configs remain available for comparison and regression.
const config: webpack.Configuration = {
  mode: 'development',
  devtool: 'source-map',
}

const nativeKernel =
  process.env.DESKTOP_KERNEL_TEST === '1'
    ? merge({}, common.nativeKernel, config, {
        entry: {
          'native-kernel': [
            path.resolve(__dirname, 'test/native-kernel/seed-profile.js'),
            path.resolve(__dirname, 'src/native-kernel/renderer'),
          ],
        },
      })
    : merge({}, common.nativeKernel, config)

// eslint-disable-next-line no-restricted-syntax
export default [merge({}, common.main, config), nativeKernel]
