import Foundation

enum Runtime {
  case Server
  case Client
}

let configServerLong: String = "ffmpeg-over-ip.server.json"
let configServerShort: String = "config.server.json"

let configClientLong: String = "ffmpeg-over-ip.client.json"
let configClientShort: String = "config.client.json"

func getConfigPaths(runtime: Runtime) -> [String] {
  let configLong = runtime == .Server ? configServerLong : configClientLong
  let configShort = runtime == .Server ? configServerShort : configClientShort

  let currentDirectory: String = FileManager.default.currentDirectoryPath
  let userDirectory: String = FileManager.default.homeDirectoryForCurrentUser.path

  var paths: [(directory: String, file: String)] = [
    (directory: currentDirectory, file: configLong),
  ]

  if userDirectory != "" {
    let userConfigDirectory: String = NSString.path(withComponents: [userDirectory, ".config"])
    let userScopedConfigDirectory: String = NSString.path(withComponents: [userConfigDirectory, "ffmpeg-over-ip"])

    paths.append((directory: userScopedConfigDirectory, file: configShort))
    paths.append((directory: userConfigDirectory, file: configLong))
  }

  paths.append((directory: "/etc/ffmpeg-over-ip", file: configShort))
  paths.append((directory: "/etc", file: configLong))

  // Collapse the paths into a single array
  let filePaths: [String] = paths.map { (directory: String, file: String) -> String in
    NSString.path(withComponents: [directory, file])
  }

  return filePaths
}

func getActiveConfigPath(filePaths: [String]) -> String? {
  var configPath: String? = nil
  // Find the first file that exists from the paths
  for filePath in filePaths {
    if FileManager.default.fileExists(atPath: filePath) {
      configPath = filePath
      break
    }
  }
  if configPath == nil {
    return nil
  }

  return configPath
}

struct ServerConfig : Codable {
  var disableLog: Bool
  var logPath: String
  var listenAddress: String
  var listenPort: Int
  var authSecret: String
  var ffmpegPath: String
  var rewrites: [[String]]
}

func loadServerConfig(configPath: String) throws -> ServerConfig {
  let fileURL = URL(fileURLWithPath: configPath)
  let data = try Data(contentsOf: fileURL)
  let decoder = JSONDecoder()
  let config =  try decoder.decode(ServerConfig.self, from: data)
  return config
}
