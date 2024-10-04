
# v3.0.0

- Change the lookup order of the configuration files (it looks for specific paths before general paths.)
  Use `--debug-print-search-paths` to see the paths it will look for the configuration files.

# v2.0.0

- Move dependencies to devDependencies since they are already bundled
- Rename `pathMappings` to `rewrites`
- Register both `ffmpeg-over-ip-server` and `ffmpeg-over-ip-client` as npm executables
- Do not search for config file in same directory as executable (this messes up `pkg` etc)

# v1.0.0

- Initial release
