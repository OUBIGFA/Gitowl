import { join, isAbsolute } from 'path'
import { installNodeDatabase } from './node-database'
import { importLegacyExport } from './import-legacy'

// Dedicated process imports into a caller-owned staging profile. Native Node
// localStorage is opened there by the command line before any stores load.
const [source, destination] = process.argv.slice(2)
if (!isAbsolute(source ?? '') || !isAbsolute(destination ?? '')) {
  throw new Error(
    'Migration requires explicit absolute source and staging paths'
  )
}
installNodeDatabase(destination)
void importLegacyExport(source, join(destination, 'migration.json')).catch(
  () => {
    console.error(
      'Legacy data migration failed; the original profile was preserved'
    )
    process.exitCode = 1
  }
)
