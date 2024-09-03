public let HELP_SERVER_VERSION: String = "ffmpeg-over-ip server 3.0.0"
public let HELP_CLIENT_VERSION: String = "ffmpeg-over-ip client 3.0.0"

public let HELP_TEXT_CLIENT: String = """
\(HELP_CLIENT_VERSION)
Usage: ffmpeg-over-ip-client [options]
       ffmpeg-over-ip-client <...ffmpeg options>

Options:
  --debug-help          Show this help message and exit
  --debug-version       Show version number and exit
  --debug-print-config  Print the configuration of the client
  --debug-print-config-paths
                        Print the paths of the configuration files
"""

public let HELP_TEXT_SERVER: String = """
\(HELP_SERVER_VERSION)
Usage: ffmpeg-over-ip-server [options]

Options:
  -h, --help, --debug-help
                        Show this help message and exit
  -v, --version, --debug-version
                        Show version number and exit
  --debug-print-config  Print the configuration of the client
  --debug-print-config-paths
                        Print the paths of the configuration files
"""
