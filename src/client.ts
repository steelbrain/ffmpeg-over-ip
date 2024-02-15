import http from 'node:http'
import { loadConfig } from './config.js'
import { CONFIG_FILE_SEARCH_PATHS_CLIENT, Runtime } from './constants.js'
import createLogger from './logger.js'

async function main() {
  if (process.argv.includes('--debug-print-search-paths')) {
    console.log(CONFIG_FILE_SEARCH_PATHS_CLIENT)
    process.exit(1)
  }

  const config = await loadConfig(Runtime.Client)

  if (process.argv.includes('--debug-print-config')) {
    console.log(config)
    process.exit(1)
  }

  const logger = createLogger(config.log)

  const request = http.request(
    {
      hostname: config.connectAddress,
      port: config.connectPort,
      path: '/',
      method: 'POST',
      headers: {
        Authorization: `Bearer ${config.authSecret}`,
      },
    },
    response => {
      if (response.statusCode !== 200) {
        process.exitCode = 1
      }
      response.on('data', chunk => {
        let parsed: unknown
        try {
          parsed = JSON.parse(chunk.toString())
        } catch (err) {
          console.error(chunk.toString())
          return
        }
        if (
          parsed != null &&
          typeof parsed === 'object' &&
          'stream' in parsed &&
          typeof parsed.stream === 'string' &&
          'data' in parsed &&
          (parsed.data == null || typeof parsed.data === 'string')
        ) {
          if (parsed.stream === 'stdout') {
            if (parsed.data != null) {
              process.stdout.write(parsed.data)
            } else {
              process.stdout.end()
            }
          } else {
            if (parsed.data != null) {
              process.stderr.write(parsed.data)
            } else {
              process.stderr.end()
            }
          }
          return
        }

        if (
          parsed != null &&
          typeof parsed === 'object' &&
          'exitCode' in parsed &&
          typeof parsed.exitCode === 'number'
        ) {
          process.exit(parsed.exitCode)
        }

        console.error(parsed)
      })
    }
  )

  request.write(JSON.stringify(process.argv.slice(2)))
  request.end()
  request.on('error', err => {
    logger.error(`Error during request: ${err}`)
    process.exitCode = 1
    console.error('Failed to connect to server')
  })
}

main().catch(err => {
  console.error('Error during initialization', { err })
  process.exit(1)
})
