import { CONFIG_FILE_ENV, CONFIG_FILE_NAMES, Runtime } from './constants'

export function loadConfig(runtime: Runtime): Config {
  const configFile = process.env[CONFIG_FILE_ENV] || CONFIG_FILE_NAMES[runtime]
  const config = require(`./${configFile}`)
  return config
}
