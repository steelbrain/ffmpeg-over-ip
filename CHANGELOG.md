# v2.0.0

- Move dependencies to devDependencies since they are already bundled
- Rename `pathMappings` to `rewrites`
- Register both `ffmpeg-over-ip-server` and `ffmpeg-over-ip-client` as npm executables
- Do not search for config file in same directory as executable (this messes up `pkg` etc)

# v1.0.0

- Initial release
