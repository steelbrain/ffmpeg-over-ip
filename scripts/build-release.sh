#!/usr/bin/env bash
#
# build-release.sh — Build fully-featured ffmpeg using jellyfin-ffmpeg's build system.
#
# Injects fio patches into the jellyfin-ffmpeg source tree, then delegates to
# jellyfin's builder scripts which handle all HW acceleration dependencies
# (NVENC/NVDEC, AMF, QSV, VAAPI, Vulkan, VideoToolbox, etc.).
#
# Usage:
#   ./scripts/build-release.sh <target>
#
# Targets:
#   linux64       Linux x86_64       (Docker)
#   linuxarm64    Linux ARM64        (Docker)
#   win64         Windows x64        (Docker, cross-compile)
#   macarm64      macOS ARM64        (native, requires Xcode)
#   mac64         macOS x86_64       (native, cross-compile on ARM)
#
# Environment:
#   GHCR_REPO     GHCR image repo for caching (e.g. ghcr.io/myorg/myrepo)
#                 If set, images are pulled from / pushed to this registry.
#                 Only used for Docker-based builds.
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
VENDOR_DIR="$ROOT/third_party/jellyfin-ffmpeg"
BUILDER_DIR="$VENDOR_DIR/builder"

GHCR_REPO="${GHCR_REPO:-}"
LOCAL_REPO="localhost/ffmpeg-over-ip"

if [[ $# -lt 1 ]]; then
    echo "Usage: $0 <target>"
    echo "Targets: linux64, linuxarm64, win64, macarm64, mac64"
    exit 1
fi

TARGET="$1"

# Validate target
case "$TARGET" in
    linux64|linuxarm64|win64|macarm64|mac64) ;;
    *)
        echo "Unknown target: $TARGET"
        echo "Valid targets: linux64, linuxarm64, win64, macarm64, mac64"
        exit 1
        ;;
esac

# --- Helper: pull cached image or build locally ---
build_or_pull() {
    local local_tag="$1"
    shift

    if [[ -n "$GHCR_REPO" ]]; then
        local ghcr_tag="${local_tag/$LOCAL_REPO/$GHCR_REPO}"
        echo "  Trying cached image $ghcr_tag..."
        if docker pull "$ghcr_tag" 2>/dev/null; then
            docker tag "$ghcr_tag" "$local_tag"
            echo "  Using cached image."
            return 0
        fi
        echo "  No cached image found, building..."
    fi

    docker build -t "$local_tag" "$@"
}

# --- Helper: push image to GHCR cache ---
push_cache() {
    local local_tag="$1"

    if [[ -n "$GHCR_REPO" ]]; then
        local ghcr_tag="${local_tag/$LOCAL_REPO/$GHCR_REPO}"
        echo "  Pushing $ghcr_tag..."
        docker tag "$local_tag" "$ghcr_tag"
        docker push "$ghcr_tag"
    fi
}

# --- Read version from debian/changelog ---
PKG_VER=0.0.0
while IFS= read -r line; do
    if [[ $line == jellyfin-ffmpeg* ]]; then
        if [[ $line =~ \(([^\)]+)\) ]]; then
            PKG_VER="${BASH_REMATCH[1]}"
            break
        fi
    fi
done < "$VENDOR_DIR/debian/changelog"
ARTIFACT_NAME="jellyfin-ffmpeg_${PKG_VER}_portable_${TARGET}-gpl"

# --- Reset submodule to clean state ---
echo "=== Resetting submodule ==="
cd "$VENDOR_DIR"
git checkout . 2>/dev/null
git clean -fdx 2>/dev/null
# Clean runtime build directory (not tracked by git, survives git clean)
rm -rf "$BUILDER_DIR/build"
cd "$ROOT"
echo "  Submodule reset to clean state"
echo ""

# --- Inject fio into source tree ---
echo "=== Injecting fio layer ==="

# Copy fio source into libavformat
cp "$ROOT/fio/fio.c" "$VENDOR_DIR/libavformat/fio.c"
cp "$ROOT/fio/fio.h" "$VENDOR_DIR/libavformat/fio.h"
echo "  Copied fio.c and fio.h into libavformat/"

# Copy our patch files into debian/patches
for patch in "$ROOT/patches"/*.patch; do
    cp "$patch" "$VENDOR_DIR/debian/patches/$(basename "$patch")"
done
echo "  Copied fio patches into debian/patches/"

# Prepend our patches to the series file (apply before jellyfin's)
SERIES="$VENDOR_DIR/debian/patches/series"
TMP="$(mktemp)"
for patch in "$ROOT/patches"/*.patch; do
    echo "$(basename "$patch")"
done > "$TMP"
cat "$SERIES" >> "$TMP"
mv "$TMP" "$SERIES"
echo "  Prepended fio patches to debian/patches/series"
echo ""

# --- macOS: native build via buildmac.sh ---
if [[ "$TARGET" == mac* ]]; then
    if [[ "$(uname -s)" != "Darwin" ]]; then
        echo "Error: macOS targets must be built on macOS"
        exit 1
    fi

    # Map target to arch for buildmac.sh
    case "$TARGET" in
        macarm64) ARCH="arm64" ;;
        mac64)    ARCH="x86_64" ;;
    esac

    # Ensure /opt/ffbuild/prefix exists (buildmac.sh hardcodes this)
    FFBUILD_PREFIX="/opt/ffbuild/prefix"
    if [[ ! -d "$FFBUILD_PREFIX" ]]; then
        echo "Creating $FFBUILD_PREFIX (requires sudo)..."
        sudo mkdir -p "$FFBUILD_PREFIX"
        sudo chown "$(whoami)" "$FFBUILD_PREFIX"
    fi

    # Add helper scripts to PATH (build scripts expect them without .sh extension)
    HELPER_BIN="$ROOT/build/mac-bin"
    mkdir -p "$HELPER_BIN"
    ln -sf "$BUILDER_DIR/images/base/git-mini-clone.sh" "$HELPER_BIN/git-mini-clone"
    ln -sf "$BUILDER_DIR/images/base/retry-tool.sh" "$HELPER_BIN/retry-tool"
    ln -sf "$BUILDER_DIR/images/base/check-wget.sh" "$HELPER_BIN/check-wget"
    export PATH="$HELPER_BIN:$PATH"

    # Install brew dependencies that 00-dep.sh would normally handle
    echo "=== Checking brew dependencies ==="
    BREW_DEPS=(nasm automake autoconf cmake meson ninja pkg-config coreutils libtool gnu-sed gnu-tar quilt texinfo wget)
    MISSING=()
    for dep in "${BREW_DEPS[@]}"; do
        brew list "$dep" &>/dev/null || MISSING+=("$dep")
    done
    if [[ ${#MISSING[@]} -gt 0 ]]; then
        echo "  Installing: ${MISSING[*]}"
        brew install "${MISSING[@]}"
    else
        echo "  All dependencies present"
    fi

    echo "=== Building ffmpeg for $TARGET (native) ==="
    cd "$BUILDER_DIR"

    # Vendored build scripts assume set -xe (no -u), so disable nounset
    set +u

    # We inline buildmac.sh logic here instead of calling it directly,
    # because its 00-dep.sh is designed for GitHub Actions runners and
    # fails on developer machines (brew uninstall, mkdir without -p, etc.).
    export BUILDER_ROOT="$BUILDER_DIR"
    VARIANT="gpl"

    # Default function stubs (normally provided by util/vars.sh)
    ffbuild_configure()    { return 0; }
    ffbuild_unconfigure()  { return 0; }
    ffbuild_cflags()       { return 0; }
    ffbuild_uncflags()     { return 0; }
    ffbuild_cxxflags()     { return 0; }
    ffbuild_uncxxflags()   { return 0; }
    ffbuild_ldflags()      { return 0; }
    ffbuild_unldflags()    { return 0; }
    ffbuild_ldexeflags()   { return 0; }
    ffbuild_unldexeflags() { return 0; }
    ffbuild_libs()         { return 0; }
    ffbuild_unlibs()       { return 0; }

    source "variants/${TARGET}-gpl.sh"

    # Collect configure flags from all dependency scripts
    get_output() {
        (
            SELF="$1"
            source "$1"
            if ffbuild_enabled; then
                ffbuild_"$2" || exit 0
            else
                ffbuild_un"$2" || exit 0
            fi
        )
    }

    for script in scripts.d/*.sh; do
        FF_CONFIGURE+=" $(get_output "$script" configure)"
        FF_CFLAGS+=" $(get_output "$script" cflags)"
        FF_CXXFLAGS+=" $(get_output "$script" cxxflags)"
        FF_LDFLAGS+=" $(get_output "$script" ldflags)"
        FF_LDEXEFLAGS+=" $(get_output "$script" ldexeflags)"
        FF_LIBS+=" $(get_output "$script" libs)"
    done

    FF_CONFIGURE="$(xargs <<< "$FF_CONFIGURE")"
    FF_CFLAGS="$(xargs <<< "$FF_CFLAGS")"
    FF_CXXFLAGS="$(xargs <<< "$FF_CXXFLAGS")"
    FF_LDFLAGS="$(xargs <<< "$FF_LDFLAGS")"
    FF_LDEXEFLAGS="$(xargs <<< "$FF_LDEXEFLAGS")"
    FF_LIBS="$(xargs <<< "$FF_LIBS")"
    FF_HOST_CFLAGS="$(xargs <<< "$FF_HOST_CFLAGS")"
    FF_HOST_LDFLAGS="$(xargs <<< "$FF_HOST_LDFLAGS")"
    FFBUILD_TARGET_FLAGS="$(xargs <<< "$FFBUILD_TARGET_FLAGS")"

    # Build base macOS dependencies (gettext, brotli, libpng — skip 00-dep.sh)
    mkdir -p build
    for macbase in images/macos/*.sh; do
        [[ "$(basename "$macbase")" == "00-dep.sh" ]] && continue
        cd "$BUILDER_ROOT"/build
        source "$BUILDER_ROOT"/"$macbase"
        ffbuild_macbase || exit $?
    done

    # Build all library dependencies from source
    cd "$BUILDER_ROOT"
    for lib in scripts.d/*.sh; do
        cd "$BUILDER_ROOT"/build
        source "$BUILDER_ROOT"/"$lib"
        ffbuild_enabled || continue
        ffbuild_dockerbuild || exit $?
    done

    # Apply patches and build ffmpeg
    cd "$BUILDER_ROOT"/..
    if [[ -f "debian/patches/series" && ! -L "patches" ]]; then
        ln -s debian/patches patches
    fi
    quilt push -a

    ./configure --prefix=/ffbuild/prefix \
        $FFBUILD_TARGET_FLAGS \
        --host-cflags="$FF_HOST_CFLAGS" \
        --host-ldflags="$FF_HOST_LDFLAGS" \
        --extra-version="Jellyfin" \
        --extra-cflags="$FF_CFLAGS" \
        --extra-cxxflags="$FF_CXXFLAGS" \
        --extra-ldflags="$FF_LDFLAGS" \
        --extra-ldexeflags="$FF_LDEXEFLAGS" \
        --extra-libs="$FF_LIBS" \
        $FF_CONFIGURE
    make -j"$(sysctl -n hw.ncpu)"

    # Package artifacts
    mkdir -p "$BUILDER_DIR/artifacts"
    tar -cJf "$BUILDER_DIR/artifacts/${ARTIFACT_NAME}.tar.xz" ffmpeg ffprobe

    echo ""
    echo "=== Build complete ==="
    echo "Artifacts in: $BUILDER_DIR/artifacts/"
    ls -lh "$BUILDER_DIR/artifacts/"
    exit 0
fi

# --- Docker-based builds (Linux, Windows) ---
echo "=== Preparing Docker images for $TARGET ==="
cd "$BUILDER_DIR"

echo "Base image..."
build_or_pull "$LOCAL_REPO/base:latest" "images/base/"
push_cache "$LOCAL_REPO/base:latest"

echo "Base-$TARGET image..."
build_or_pull "$LOCAL_REPO/base-$TARGET:latest" \
    --build-arg "GH_REPO=$LOCAL_REPO" "images/base-$TARGET/"
push_cache "$LOCAL_REPO/base-$TARGET:latest"

echo "Generating Dockerfile for $TARGET-gpl..."
REGISTRY_OVERRIDE=localhost GITHUB_REPOSITORY="ffmpeg-over-ip" \
    ./generate.sh "$TARGET" gpl

# Remove --link from COPY instructions — incompatible with overlayfs-on-overlayfs (Docker-in-K8s)
sed -i 's/COPY --link /COPY /g' Dockerfile

echo "$TARGET-gpl image (all HW acceleration deps)..."
build_or_pull "$LOCAL_REPO/$TARGET-gpl:latest" "."
push_cache "$LOCAL_REPO/$TARGET-gpl:latest"

echo ""
echo "=== Images ready ==="
echo ""

# --- Build ffmpeg inside container ---
echo "=== Building ffmpeg ==="
REGISTRY_OVERRIDE=localhost GITHUB_REPOSITORY="ffmpeg-over-ip" \
    ./build.sh "$TARGET" gpl

echo ""
echo "=== Build complete ==="
echo "Artifacts in: $BUILDER_DIR/artifacts/"
ls -lh "$BUILDER_DIR/artifacts/"
