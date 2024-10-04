import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { z } from 'zod'

import stripJsonComments from 'strip-json-comments'

import {
  CONFIG_FILE_SEARCH_PATHS_CLIENT,
  CONFIG_FILE_SEARCH_PATHS_SERVER,
  Runtime,
} from './constants.js'
import createLogger from './logger.js'

const configSchemaServer = z
  .object({
    log: z.union([z.literal(false), z.string()]),
    listenAddress: z.string(),
    listenPort: z.number(),
    authSecret: z.string().min(15).max(100),
    ffmpegPath: z.string(),
    rewrites: z.array(z.tuple([z.string(), z.string()])),
  })
  .strict()

const configSchemaClient = z.object({
  log: z.union([z.literal(false), z.string()]),
  connectAddress: z.string(),
  connectPort: z.number(),
  authSecret: z.string().min(15).max(100),
})

export async function loadConfig<T extends Runtime.Client | Runtime.Server>(
  runtime: T
): Promise<
  T extends Runtime.Server ? z.infer<typeof configSchemaServer> : z.infer<typeof configSchemaClient>
> {
  const configFilePaths =
    runtime === Runtime.Server ? CONFIG_FILE_SEARCH_PATHS_SERVER : CONFIG_FILE_SEARCH_PATHS_CLIENT

  let selectedConfigFilePath: string | null = null
  for (const [directory, files] of configFilePaths) {
    let directoryToUse = directory
    if (directoryToUse.includes('$TMPDIR')) {
      directoryToUse = directoryToUse.replace('$TMPDIR', os.tmpdir())
    }
    for (const file of files) {
      const filePath = path.join(directory, file)
      const fileStat = await fs.promises.stat(filePath).catch(() => null)
      if (fileStat != null) {
        selectedConfigFilePath = filePath
        break
      }
    }
  }

  if (selectedConfigFilePath == null) {
    throw new Error(
      'No config file found. Try running with --debug-print-search-paths to print search paths'
    )
  }
  const schemaToUse = runtime === Runtime.Server ? configSchemaServer : configSchemaClient
  let configContent: z.infer<typeof schemaToUse>

  let configLogger: ReturnType<typeof createLogger> | null = null

  try {
    const textContents = await fs.promises.readFile(selectedConfigFilePath, 'utf-8')
    let parsed: unknown
    try {
      parsed = JSON.parse(stripJsonComments(textContents))
    } catch (_) {
      throw new Error(`Malformed JSON in config file at ${selectedConfigFilePath}`)
    }
    if (
      parsed != null &&
      typeof parsed === 'object' &&
      'log' in parsed &&
      typeof parsed.log === 'string'
    ) {
      configLogger = createLogger(parsed.log)
    }

    configContent = schemaToUse.parse(parsed)
  } catch (err) {
    const message = `Failed to read config file at ${selectedConfigFilePath}: ${err}`
    configLogger?.error(message)

    throw new Error(message)
  }

  return configContent as T extends Runtime.Server
    ? z.infer<typeof configSchemaServer>
    : z.infer<typeof configSchemaClient>
}

export function rewriteArgsInServer(
  args: string[],
  config: z.infer<typeof configSchemaServer>
): string[] {
  return args.slice().map(item => {
    let arg = item
    for (const [from, to] of config.rewrites) {
      arg = arg.replace(from, to)
    }
    return arg
  })
}
