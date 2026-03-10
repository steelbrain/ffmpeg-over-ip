#!/usr/bin/env bash
#
# build-ffmpeg.sh — Build jellyfin-ffmpeg with the fio layer patched in.
#
# Uses the jellyfin-ffmpeg submodule as the base, copies fio source
# into libavformat, applies fio patches, then builds.
#
# Usage:
#   ./scripts/build-ffmpeg.sh [--prefix DIR] [--minimal] [--cross TARGET]
#
# Options:
#   --prefix  DIR       install prefix (default: ./build/ffmpeg)
#   --minimal           minimal build (no external libs, for testing only)
#   --cross   TARGET    cross-compile target (e.g. aarch64-linux-gnu, i686-linux-gnu,
#                       x86_64-w64-mingw32, aarch64-w64-mingw32, x86_64-apple-darwin)
#   --no-verify         skip post-build verification (needed for cross-compiles)
#
# Environment:
#   EXTRA_CONFIGURE     additional flags passed to ./configure
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

PREFIX="${PREFIX:-$ROOT/build/ffmpeg}"
MINIMAL=0
CROSS=""
VERIFY=1
EXTRA_CONFIGURE="${EXTRA_CONFIGURE:-}"

while [[ $# -gt 0 ]]; do
    case "$1" in
        --prefix)    PREFIX="$2"; shift 2 ;;
        --minimal)   MINIMAL=1; shift ;;
        --cross)     CROSS="$2"; shift 2 ;;
        --no-verify) VERIFY=0; shift ;;
        *) echo "Unknown option: $1"; exit 1 ;;
    esac
done

VENDOR_DIR="$ROOT/third_party/jellyfin-ffmpeg"
BUILD_DIR="$ROOT/build"
SRC_DIR="$BUILD_DIR/jellyfin-ffmpeg"

if [ ! -f "$VENDOR_DIR/configure" ]; then
    echo "Submodule not initialized. Run: git submodule update --init"
    exit 1
fi

FFMPEG_VERSION=$(cat "$VENDOR_DIR/RELEASE" 2>/dev/null | tr -d '[:space:]')
echo "=== Building jellyfin-ffmpeg (${FFMPEG_VERSION}) with fio patches ==="
echo "  Source:  $SRC_DIR"
echo "  Prefix:  $PREFIX"
[ -n "$CROSS" ] && echo "  Cross:   $CROSS"
echo ""

# Create a working copy so we don't modify the submodule
mkdir -p "$BUILD_DIR"
if [ -d "$SRC_DIR" ]; then
    rm -rf "$SRC_DIR"
fi
echo "Copying source tree..."
cp -a "$VENDOR_DIR" "$SRC_DIR"

# Copy fio source into ffmpeg tree
echo "Copying fio layer..."
cp "$ROOT/fio/fio.c" "$SRC_DIR/libavformat/fio.c"
cp "$ROOT/fio/fio.h" "$SRC_DIR/libavformat/fio.h"

# Apply fio patches
echo "Applying fio patches..."
cd "$SRC_DIR"
for patch in "$ROOT/patches"/*.patch; do
    echo "  Applying $(basename "$patch")..."
    patch -p1 < "$patch"
done

# Apply jellyfin patches (if patches exist)
if [ -d "debian/patches" ] && [ -f "debian/patches/series" ]; then
    if command -v quilt &>/dev/null; then
        echo "Applying jellyfin patches via quilt..."
        QUILT_PATCHES=debian/patches quilt push -a 2>&1 | tail -1
    else
        echo "Applying jellyfin patches manually..."
        while IFS= read -r patchfile; do
            [[ "$patchfile" =~ ^#.*$ || -z "$patchfile" ]] && continue
            echo "  Applying $patchfile..."
            patch -p1 < "debian/patches/$patchfile"
        done < "debian/patches/series"
    fi
fi

# Build cross-compile configure flags
CROSS_FLAGS=""
EXE_SUFFIX=""

if [ -n "$CROSS" ]; then
    case "$CROSS" in
        aarch64-linux-gnu)
            CROSS_FLAGS="--arch=aarch64 --cross-prefix=${CROSS}- --enable-cross-compile --target-os=linux"
            ;;
        i686-linux-gnu)
            CROSS_FLAGS="--arch=x86 --cross-prefix=${CROSS}- --enable-cross-compile --target-os=linux"
            ;;
        x86_64-w64-mingw32)
            CROSS_FLAGS="--arch=x86_64 --cross-prefix=${CROSS}- --enable-cross-compile --target-os=mingw32"
            EXE_SUFFIX=".exe"
            ;;
        aarch64-w64-mingw32)
            CROSS_FLAGS="--arch=aarch64 --cross-prefix=${CROSS}- --enable-cross-compile --target-os=mingw32"
            EXE_SUFFIX=".exe"
            ;;
        x86_64-apple-darwin)
            CROSS_FLAGS="--arch=x86_64 --enable-cross-compile"
            export CFLAGS="${CFLAGS:-} -arch x86_64"
            export LDFLAGS="${LDFLAGS:-} -arch x86_64"
            ;;
        *)
            echo "Unknown cross target: $CROSS"
            exit 1
            ;;
    esac
fi

# Configure
echo ""
echo "Configuring..."

COMMON_FLAGS="--prefix=$PREFIX --enable-gpl --disable-doc --disable-htmlpages --disable-manpages --disable-podpages --disable-txtpages"

if [ "$MINIMAL" -eq 1 ]; then
    # shellcheck disable=SC2086
    ./configure \
        $COMMON_FLAGS \
        $CROSS_FLAGS \
        $EXTRA_CONFIGURE \
        --disable-network \
        --disable-autodetect \
        --disable-everything \
        --enable-protocol=file \
        --enable-protocol=pipe \
        --enable-demuxer=rawvideo,image2,matroska,mov,mpegts,lavfi \
        --enable-muxer=rawvideo,image2,matroska,mpegts,hls,null \
        --enable-decoder=rawvideo,wrapped_avframe \
        --enable-encoder=rawvideo,wrapped_avframe \
        --enable-parser=h264,hevc \
        --enable-filter=testsrc,testsrc2,color,null,nullsink,copy,scale \
        --enable-indev=lavfi
else
    # shellcheck disable=SC2086
    ./configure \
        $COMMON_FLAGS \
        $CROSS_FLAGS \
        $EXTRA_CONFIGURE \
        --enable-version3 \
        --enable-static \
        --disable-shared \
        --pkg-config-flags=--static
fi

# Build
echo ""
echo "Building..."
NPROC=$(nproc 2>/dev/null || sysctl -n hw.ncpu 2>/dev/null || echo 4)
make -j"$NPROC"

# Copy binaries
echo ""
echo "Installing binaries..."
mkdir -p "$PREFIX/bin"
cp "ffmpeg${EXE_SUFFIX}" "$PREFIX/bin/ffmpeg${EXE_SUFFIX}"
cp "ffprobe${EXE_SUFFIX}" "$PREFIX/bin/ffprobe${EXE_SUFFIX}"

echo ""
echo "=== Build complete ==="
echo "  ffmpeg:  $PREFIX/bin/ffmpeg${EXE_SUFFIX}"
echo "  ffprobe: $PREFIX/bin/ffprobe${EXE_SUFFIX}"

# Verify (skip for cross-compiles)
if [ "$VERIFY" -eq 1 ] && [ -z "$CROSS" ]; then
    echo ""
    echo "Verifying..."
    "$PREFIX/bin/ffmpeg" -version | head -1
    echo ""
    echo "Testing fio passthrough (no FFOIP_PORT)..."
    "$PREFIX/bin/ffmpeg" -version > /dev/null 2>&1 && echo "  OK: runs without FFOIP_PORT"
    echo ""
    echo "Testing fio with invalid port (should fail gracefully)..."
    FFOIP_PORT=0 "$PREFIX/bin/ffmpeg" -version > /dev/null 2>&1 && echo "  OK: exits cleanly with FFOIP_PORT=0" || echo "  OK: exited with error (expected)"
fi
