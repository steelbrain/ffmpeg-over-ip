#!/bin/sh
# install-client.sh — Install the ffmpeg-over-ip client in the current directory.
#
# Usage:
#   curl -fsSL https://ffmpeg-over-ip.com/install-client.sh | sh
#
# Re-runs are idempotent: each step (download, ffprobe symlink, config) is
# skipped if its output already exists. Set FOIP_FORCE=1 to re-download.

set -eu

REPO="steelbrain/ffmpeg-over-ip"
ROLE="client"
BINARY="ffmpeg-over-ip-${ROLE}"
CONFIG="ffmpeg-over-ip.${ROLE}.jsonc"

# --- Platform detection -----------------------------------------------------

case "$(uname -s)" in
    Linux)  os="linux" ;;
    Darwin) os="macos" ;;
    *) echo "Unsupported OS: $(uname -s)" >&2; exit 1 ;;
esac
case "$(uname -m)" in
    x86_64|amd64)  arch="amd64" ;;
    arm64|aarch64) arch="arm64" ;;
    *) echo "Unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac
platform="${os}-${arch}"

# --- Helpers ----------------------------------------------------------------

# Read a value from the terminal, optionally with a default.
# Aborts the whole script (via SIGTERM to $$) on EOF — better than looping
# forever in a non-interactive shell.
prompt() {
    label="$1"; default="$2"
    if [ -n "$default" ]; then
        printf '%s [default=%s]: ' "$label" "$default" > /dev/tty
    else
        printf '%s: ' "$label" > /dev/tty
    fi
    if ! IFS= read -r value < /dev/tty; then
        echo "" > /dev/tty
        echo "Unable to prompt user for interactive input. Aborting." >&2
        kill -TERM $$ 2>/dev/null
        exit 1
    fi
    # Strip control chars to keep the JSON config valid.
    value=$(printf '%s' "$value" | tr -d '\000-\037\177')
    if [ -z "$value" ] && [ -n "$default" ]; then
        value="$default"
    fi
    printf '%s' "$value"
}

prompt_required() {
    while :; do
        value=$(prompt "$1" "")
        if [ -n "$value" ]; then
            printf '%s' "$value"
            return
        fi
        echo "Value is required." > /dev/tty
    done
}

# --- Download + extract -----------------------------------------------------

if [ -e "$BINARY" ] && [ -z "${FOIP_FORCE:-}" ]; then
    echo "$BINARY already exists, skipping download. Set FOIP_FORCE=1 to overwrite."
else
    command -v unzip >/dev/null 2>&1 || {
        echo "Required command 'unzip' not found. Install it and re-run." >&2
        exit 1
    }
    tmp_dir="$(mktemp -d 2>/dev/null || mktemp -d -t foip)"
    trap 'rm -rf "$tmp_dir"' EXIT INT TERM
    tmp_zip="$tmp_dir/release.zip"
    url="https://github.com/${REPO}/releases/latest/download/${platform}-ffmpeg-over-ip-${ROLE}.zip"

    echo "Downloading ${platform}-ffmpeg-over-ip-${ROLE}.zip ..."
    if command -v curl >/dev/null 2>&1; then
        curl -fSL "$url" -o "$tmp_zip"
    elif command -v wget >/dev/null 2>&1; then
        # No -q so download progress and HTTP errors stay visible.
        wget -O "$tmp_zip" "$url"
    else
        echo "Need either curl or wget to download the release." >&2
        exit 1
    fi

    echo "Extracting..."
    unzip -o -q "$tmp_zip" -d "$tmp_dir/extract"
    # The release zip wraps everything in a single top-level directory.
    extract_root="$tmp_dir/extract/${platform}-ffmpeg-over-ip-${ROLE}"
    if [ ! -d "$extract_root" ]; then
        echo "Unexpected zip layout: missing $extract_root" >&2
        exit 1
    fi
    # cp -R src/. . copies contents (incl. hidden files) without the wrapper.
    cp -Rf "$extract_root"/. .
    rm -rf "$tmp_dir"
    trap - EXIT INT TERM
fi

# Verify the extraction produced what we expect — guards against a malformed
# release artifact or a partial download.
if [ ! -e "$BINARY" ]; then
    echo "Expected $BINARY in current directory but it's missing." >&2
    echo "The release zip may be malformed; retry with FOIP_FORCE=1." >&2
    exit 1
fi

# --- Make executable + strip macOS quarantine -------------------------------

chmod +x "$BINARY"

if [ "$os" = "macos" ] && command -v xattr >/dev/null 2>&1; then
    xattr -dr com.apple.quarantine "$BINARY" 2>/dev/null || true
fi

# --- ffprobe symlink --------------------------------------------------------

if [ -e ffprobe ] || [ -L ffprobe ]; then
    echo "ffprobe already exists, leaving alone."
else
    ln -s "$BINARY" ffprobe
    echo "Created ffprobe -> $BINARY symlink."
fi

# --- Config -----------------------------------------------------------------

if [ -e "$CONFIG" ]; then
    echo "$CONFIG already exists, leaving alone."
else
    if ! { : > /dev/tty; } 2>/dev/null; then
        echo "Unable to prompt user for interactive input." >&2
        echo "Create $CONFIG manually using the template at:" >&2
        echo "  https://github.com/${REPO}/blob/main/template.ffmpeg-over-ip.${ROLE}.jsonc" >&2
        exit 1
    fi

    echo ""
    echo "Configuring client. These values must match the server."
    server_host=$(prompt_required "Server host or IP")
    server_port=$(prompt "Server port" "5050")
    auth_secret=$(prompt_required "Auth secret (must match the server)")

    # printf %s inserts arguments verbatim, so user-supplied $, `, \ are safe.
    # We still escape \ and " for valid JSON.
    esc_host=$(printf '%s' "$server_host" | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g')
    esc_port=$(printf '%s' "$server_port" | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g')
    esc_secret=$(printf '%s' "$auth_secret" | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g')

    printf '{\n  "log": "stdout",\n  "address": "%s:%s",\n  "authSecret": "%s"\n}\n' \
        "$esc_host" "$esc_port" "$esc_secret" > "$CONFIG"
    echo "Wrote $CONFIG."
fi

echo ""
echo "Done. Verify with:"
echo "  ./$BINARY -version"
