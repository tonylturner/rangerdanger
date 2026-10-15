#!/bin/sh
# RangerDanger IEC 60870-5-104 programs
# Copyright (C) 2026 The RangerDanger contributors
# SPDX-License-Identifier: GPL-3.0-or-later
#
# Build the GPLv3 static iec104cmd binary for the Kali and eng-ws images.
set -eu

if [ "$#" -ne 3 ]; then
    echo "usage: $0 SOURCE_DIR OUTPUT_DIR BUILD_DIR" >&2
    exit 2
fi

source_dir=$1
output_dir=$2
build_dir=$3
archive="$build_dir/lib60870.tar.gz"
lib_source="$build_dir/lib60870"
lib_prefix="$build_dir/lib60870-install"

mkdir -p "$output_dir" "$build_dir"
curl -fsSL -o "$archive" \
    "https://github.com/mz-automation/lib60870/archive/refs/tags/v2.4.1.tar.gz"
echo "d5708b9885c2c068ac9d9bae4696a624bd1d1d6645a1b1a9261c00274bc36c3a  $archive" \
    | sha256sum -c -
mkdir -p "$lib_source"
tar -xzf "$archive" -C "$lib_source" --strip-components=1
make -C "$lib_source/lib60870-C" -j"$(nproc)"
make -C "$lib_source/lib60870-C" install "INSTALL_PREFIX=$lib_prefix"

cc -std=gnu11 -D_GNU_SOURCE -O2 -Wall -Wextra -Werror -static \
    -I"$source_dir/src" -I"$lib_prefix/include" \
    -o "$output_dir/iec104cmd" \
    "$source_dir/src/iec104cmd.c" \
    "$source_dir/src/common.c" \
    "$source_dir/src/client.c" \
    "$lib_prefix/lib/liblib60870.a" \
    -lpthread -lm
