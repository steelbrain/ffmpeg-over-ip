import { CONFIG_FILE_SEARCH_PATHS_SERVER } from './constants'

async function main() {
  if (process.argv.includes('--internal-print-search-paths')) {
    console.log(CONFIG_FILE_SEARCH_PATHS_SERVER)
    process.exit(1)
  }
}

main().catch(err => {
  console.error('Error during initialization', { err })
  process.exit(1)
})
