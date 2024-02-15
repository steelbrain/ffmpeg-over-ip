import fs from 'node:fs'

export default function createLogger(logFile: string | false) {
  function logToOutput(prefix: string, message: string) {
    const prefixToUse = `${new Date().toISOString()} ${prefix}`

    if (logFile === false) {
      return
    }
    if (logFile === 'stdout') {
      console.log(prefixToUse, message)
      return
    }
    if (logFile === 'error') {
      console.error(prefixToUse, message)
      return
    }
    fs.appendFile(logFile, `${prefixToUse} ${message}\n`, err => {
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
