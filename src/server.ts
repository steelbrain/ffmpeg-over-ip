import { loadConfig } from './config.js'
import { CONFIG_FILE_SEARCH_PATHS_SERVER, Runtime } from './constants.js'

async function main() {
  if (process.argv.includes('--debug-print-search-paths')) {
    console.log(CONFIG_FILE_SEARCH_PATHS_SERVER)
    process.exit(1)
  }

  const config = await loadConfig(Runtime.Server)

  if (process.argv.includes('--debug-print-config')) {
    console.log(config)
    process.exit(1)
  }
}

main().catch(err => {
  console.error('Error during initialization', { err })
  process.exit(1)
})
