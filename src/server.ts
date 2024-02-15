import childProcess from 'node:child_process'
import cluster from 'node:cluster'
import http from 'node:http'
import os from 'node:os'

import streamToPromise from 'sb-stream-promise'

import { loadConfig, rewriteArgsInServer } from './config.js'
import {
  CONFIG_FILE_SEARCH_PATHS_SERVER,
  Runtime,
  SERVER_CLUSTER_RESTART_DELAY_MS,
  SERVER_MAX_PAYLOAD_SIZE_BYTES,
} from './constants.js'
import createLogger from './logger.js'

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

  const logger = createLogger(config.log)

  if (cluster.isPrimary) {
    const numForks = os.availableParallelism()
    for (let i = 0; i < numForks; i += 1) {
      cluster.fork()
    }

    cluster.on('exit', (worker, code, signal) => {
      logger.log(
        `Worker process died pid=${worker.process.pid} code=${code} signal=${signal} -- respawning`
      )
      setTimeout(cluster.fork.bind(cluster), SERVER_CLUSTER_RESTART_DELAY_MS)
    })

    logger.log(
      `Server started with ${numForks} workers at ${config.listenAddress}:${config.listenPort}`
    )

    return
  }

  const activeRequests = new Map<http.ServerResponse, null | childProcess.ChildProcess>()
  http
    .createServer((req, res) => {
      if (req.headers.authorization !== `Bearer ${config.authSecret}`) {
        res.setHeader('Content-Type', 'application/json')
        res.writeHead(401)
        res.end(JSON.stringify({ error: 'Unauthorized' }))
        logger.error('Rejected request: Unauthorized')
        return
      }
      if (req.method !== 'POST') {
        res.setHeader('Content-Type', 'application/json')
        res.writeHead(405)
        res.end(JSON.stringify({ error: 'Method Not Allowed' }))
        logger.error('Rejected request: Method not allowed')
        return
      }

      let activeRequest: null | childProcess.ChildProcess = null

      res.on('close', () => {
        activeRequest?.kill()
        activeRequests.delete(res)
      })
      activeRequests.set(res, null)

      streamToPromise(req, SERVER_MAX_PAYLOAD_SIZE_BYTES)
        .then(requestBody => {
          let parsed: unknown
          try {
            parsed = JSON.parse(requestBody)
          } catch (err) {
            throw new Error('Malformed JSON in request body')
          }
          if (!Array.isArray(parsed) || parsed.some(item => typeof item !== 'string')) {
            throw new Error('Request body MUST have a type of string[]')
          }
          return parsed as string[]
        })
        .then(
          args => {
            res.setHeader('Content-Type', 'application/json')
            res.writeHead(200)

            activeRequest = childProcess.spawn(
              config.ffmpegPath,
              rewriteArgsInServer(args, config),
              {
                stdio: ['ignore', 'pipe', 'pipe'],
              }
            )
            activeRequest.stdout?.on('data', chunk => {
              res.write(`${JSON.stringify({ stream: 'stdout', data: chunk.toString() })}\n`)
            })
            activeRequest.stdout?.on('end', () => {
              res.write(`${JSON.stringify({ stream: 'stdout', data: null })}\n`)
            })
            activeRequest.stderr?.on('data', chunk => {
              res.write(`${JSON.stringify({ stream: 'stderr', data: chunk.toString() })}\n`)
            })
            activeRequest.stderr?.on('end', () => {
              res.write(`${JSON.stringify({ stream: 'stderr', data: null })}\n`)
            })
            activeRequest.on('error', err => {
              res.end(`${JSON.stringify({ stream: 'stderr', data: err.message })}\n`)
              activeRequests.delete(res)
              activeRequest = null
            })
            activeRequest.on('exit', code => {
              res.end(`${JSON.stringify({ exitCode: code })}\n`)
              activeRequests.delete(res)
              activeRequest = null
            })
          },
          err => {
            res.setHeader('Content-Type', 'application/json')
            res.writeHead(400)
            res.end(JSON.stringify({ error: err.message }))
            logger.error('Rejected request: Bad Request')
          }
        )
    })
    .listen(config.listenPort, config.listenAddress)
}

main().catch(err => {
  console.error('Error during initialization', { err })
  process.exit(1)
})
