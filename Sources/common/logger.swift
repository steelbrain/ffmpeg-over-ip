import Foundation

enum LoggerError: Error {
  case OutputFileNotWritable
}

public class Logger {
  public var enabled: Bool
  public var logFilePath: String
  private let formatter = ISO8601DateFormatter()

  private let queue: DispatchQueue = DispatchQueue(label: "logger", qos: .background)
  private var fileHandle: FileHandle?

  public init(_ logFilePath: String, enabled: Bool) throws {
    self.enabled = enabled
    self.logFilePath = logFilePath

    if enabled && !self.isOutputWritable() {
      throw LoggerError.OutputFileNotWritable
    }
  }

  public func info(_ message: String) {
    self.log("INFO: \(message)")
  }

  public func error(_ message: String) {
    self.log("ERROR: \(message)")
  }

  private func log(_ message: String) {
    if !self.enabled {
      return
    }

    let timestamp = self.formatter.string(from: Date())
    let logLine = "\(timestamp) \(message)\n"

    if self.logFilePath == "stdout" {
      fputs(logLine, stdout)
      return
    }

    if self.logFilePath == "stderr" {
      fputs(logLine, stderr)
      return
    }

    self.queue.async {
      guard let handle: FileHandle = self.getHandle() else {
        // Retry the original log, now that we've reset the file handle.
        self.log(message)
        return
      }
      handle.seekToEndOfFile()
      handle.write(logLine.data(using: .utf8)!)
    }
  }

  private func getHandle() -> FileHandle? {
    if self.fileHandle == nil {
      do {
        self.fileHandle = try FileHandle(forWritingTo: URL(fileURLWithPath: self.logFilePath))
      } catch {
        self.logFilePath = "stderr"
        self.error("Failed to open log file. Redirecting to stderr.")
        return nil
      }
    }
    return self.fileHandle
  }

  private func isOutputWritable() -> Bool {
    if !self.enabled || self.logFilePath == "stdout" || self.logFilePath == "stderr" {
      return true
    }

    let fileExists = FileManager.default.fileExists(atPath: self.logFilePath)
    if fileExists {
      return FileManager.default.isWritableFile(atPath: self.logFilePath)
    }

    let creationResult = FileManager.default.createFile(
      atPath: self.logFilePath, contents: nil, attributes: nil)

    return creationResult
  }
}
