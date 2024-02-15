import os from 'node:os'
import path from 'node:path'

export const SERVER_MAX_PAYLOAD_SIZE_BYTES = 102400 // 100kb
export const SERVER_CLUSTER_RESTART_DELAY_MS = 5000

export enum Runtime {
  Server = 'server',
  Client = 'client',
}

export const CONFIG_FILE_ENV = 'FFMPEG_OVER_IP_CONFIG_FILE'

const CONFIG_FILE_NAMES_SERVER_FULL = ['ffmpeg-over-ip.server.json', 'ffmpeg-over-ip.server.jsonc']
const CONFIG_FILE_NAMES_SERVER_SHORT = ['config.server.json', 'config.server.jsonc']

const CONFIG_FILE_NAMES_CLIENT_FULL = ['ffmpeg-over-ip.client.json', 'ffmpeg-over-ip.client.jsonc']
const CONFIG_FILE_NAMES_CLIENT_SHORT = ['config.client.json', 'config.client.jsonc']

const CONFIG_FILE_SEARCH_PATHS: [string, string[], string[]][] = [
  ['/etc/ffmpeg-over-ip', CONFIG_FILE_NAMES_SERVER_SHORT, CONFIG_FILE_NAMES_CLIENT_SHORT],
  ['/etc', CONFIG_FILE_NAMES_SERVER_FULL, CONFIG_FILE_NAMES_CLIENT_FULL],
  [
    path.join(os.homedir(), '.config', 'ffmpeg-over-ip'),
    CONFIG_FILE_NAMES_SERVER_SHORT,
    CONFIG_FILE_NAMES_CLIENT_SHORT,
  ],
  [
    path.join(os.homedir(), '.config'),
    CONFIG_FILE_NAMES_SERVER_FULL,
    CONFIG_FILE_NAMES_CLIENT_FULL,
  ],
  [process.cwd(), CONFIG_FILE_NAMES_SERVER_FULL, CONFIG_FILE_NAMES_CLIENT_FULL],
]

if (process.argv.length > 1) {
  const execDirectory = path.dirname(`${process.argv[1]}`)
  CONFIG_FILE_SEARCH_PATHS.push([
    execDirectory,
    CONFIG_FILE_NAMES_SERVER_FULL,
    CONFIG_FILE_NAMES_CLIENT_FULL,
  ])
}

export const CONFIG_FILE_SEARCH_PATHS_SERVER: [string, string[]][] = CONFIG_FILE_SEARCH_PATHS.map(
  ([path, server]) => [path, server]
)

export const CONFIG_FILE_SEARCH_PATHS_CLIENT: [string, string[]][] = CONFIG_FILE_SEARCH_PATHS.map(
  ([path, , client]) => [path, client]
)
