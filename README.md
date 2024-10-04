# ffmpeg over IP

Connect to remote ffmpeg servers. Are you tired of unsuccessfully trying to pass your GPU through to a docker
container running in a VM? So was I! `ffmpeg-over-ip` allows you to run an ffmpeg server on a machine with access
to a GPU (Linux, Windows, or Mac) and connect to it from a remote machine. The only thing you need is Node.js
installed and a shared filesystem (could be NFS, SMB, etc.) between the two machines.

## Installation

`ffmpeg-over-ip` consists of two main parts, the server and the client. Both are packed neatly into single JS
files. You can download these from the [npm interface][1] or by `npm install ffmpeg-over-ip` and then copying
them to the relevant places. You don't need any `node_modules` to run the server or the client.

The javascript files require Node.js runtime to work. If you want standalone files that you can mount in a docker
container, you can find these in the [Github Releases][2]. On the releases page, you may have to click **"Show all
assets"** to see the files.

## Configuration

The server and the client are both configured using JSONC (JSON with comments) configuration files. The paths
of these files can be flexible. To identify which paths are being used, you can invoke either with `--debug-print-search-paths`.

Template/example configuration files are provided in this repository for your convinience. Unless the server and the client
share the same filesystem, you may have to specify `rewrites` in the server configuration file.

## Usage

Both the server and the client files are executable, so long as there is a Node.js installation available. If you intend
to use this in a docker container, you can directly mount the client file to where the container would expect a regular
ffmpeg executable to be, ie `docker run -v ./path/to/client-bin:/usr/lib/jellyfin-ffmpeg/ffmpeg ...`.

The server and the client communicate commands over HTTP, so make sure that whatever port you specify on the server is
allowed through the firewall.

Assuming you **download one of the release files**, here's what the usage would look like

On the client side

```sh
$ ./ffmpeg-over-ip-client --debug-print-search-paths # See the places where it'll look for config
$ cp template.ffmpeg-over-ip.client.jsonc ffmpeg-over-ip.client.jsonc # Add config to one of the places
$ nano ffmpeg-over-ip.client.jsonc # Change the stuff you want
$ ./ffmpeg-over-ip-client <use like ffmpeg, add ffmpeg args here>
```

On the server side

```sh
$ ./ffmpeg-over-ip-server --debug-print-search-paths # See the places where it'll look for config
$ cp template.ffmpeg-over-ip.server.jsonc ffmpeg-over-ip.server.jsonc # Add config to one of the places
$ nano ffmpeg-over-ip.server.jsonc # Change the stuff you want, especially the rewrites
$ ./ffmpeg-over-ip-server
```

Assuming you want to **download these from npm**, here's how you would do it

On the client side:

```sh
$ npm install ffmpeg-over-ip
$ ./node_modules/.bin/ffmpeg-over-ip-client --debug-print-search-paths # See the places where it'll look for config
$ cp template.ffmpeg-over-ip.client.jsonc ffmpeg-over-ip.client.jsonc # Add config to one of the places
$ nano ffmpeg-over-ip.client.jsonc # Change the stuff you want
$ ./node_modules/.bin/ffmpeg-over-ip-client <use like ffmpeg, add ffmpeg args here>
```

On the server side:

```sh
$ npm install ffmpeg-over-ip
$ ./node_modules/.bin/ffmpeg-over-ip-server --debug-print-search-paths # See the places where it'll look for config
$ cp template.ffmpeg-over-ip.server.jsonc ffmpeg-over-ip.server.jsonc # Add config to one of the places
$ nano ffmpeg-over-ip.server.jsonc # Change the stuff you want, especially the rewrites
$ ./node_modules/.bin/ffmpeg-over-ip-server
```

## License

The contents of this project are licensed under the terms of the MIT License.

[1]:https://www.npmjs.com/package/ffmpeg-over-ip?activeTab=code
[2]:https://github.com/steelbrain/ffmpeg-over-ip/releases

