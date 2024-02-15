import fs from 'node:fs'

export default function createLogger(logFile: string | false) {
  function logToOutput(prefix: string, message: string) {
    if (logFile === false) {
      return
    }
    if (logFile === 'stdout') {
      console.log(prefix, message)
      return
    }
    if (logFile === 'error') {
      console.error(prefix, message)
      return
    }
    fs.appendFile(logFile, `${prefix} ${message}\n`, err => {
      console.error(`Error writing to logfile at ${logFile}`, { err })
    })
  }

  return {
    log: (message: string) => {
      logToOutput('[INFO]', message)
    },
    error: (message: string) => {
      logToOutput('[ERROR]', message)
    },
  }
}
