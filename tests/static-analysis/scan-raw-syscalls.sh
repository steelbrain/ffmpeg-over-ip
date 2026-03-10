#!/usr/bin/env bash
# Wrapper for scan-raw-syscalls.py
exec python3 "$(dirname "$0")/scan-raw-syscalls.py" "$@"
