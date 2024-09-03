
func main() throws -> Void {
  if CommandLine.arguments.contains("-h") || CommandLine.arguments.contains("--help") || CommandLine.arguments.contains("--debug-help") {
    print(HELP_TEXT_SERVER)
    return
  }
  if CommandLine.arguments.contains("-v") || CommandLine.arguments.contains("--version") || CommandLine.arguments.contains("--debug-version") {
    print(HELP_SERVER_VERSION)
    return
  }

  let configPaths: [String] = getConfigPaths(runtime: .Server)

  if CommandLine.arguments.contains("--debug-print-config-paths") {
    print("Config lookup paths: ")
    for configPath in configPaths {
      print("  \(configPath)")
    }
    return
  }

  let configPath = getActiveConfigPath(filePaths: configPaths)

  if configPath == nil {
    print("No config file found. Try running with --debug-print-config-paths to print search paths")
    exit(1)
  }

  if CommandLine.arguments.contains("--debug-print-config") {
    print("Active Config file: \(configPath!)")
  }

  let config: ServerConfig

  do {
    config = try loadServerConfig(configPath: configPath!)
  } catch {
    print("Error loading config:")
    dump(error)
    exit(1)
  }

  if CommandLine.arguments.contains("--debug-print-config") {
    print("Active Config:")
    dump(config)
    return
  }

  print("Do the thing here")
}

do {
    try main()
} catch {
    print("Error: \(error)")
    exit(1)
}
