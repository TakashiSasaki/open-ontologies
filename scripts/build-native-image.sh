#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)
# SPDX-License-Identifier: MIT
# Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)
# Usage: scripts/build-native-image.sh IMAGE_TAG [additional docker build options]
# Requires a clean Git worktree; run through sudo if Docker requires it.
set -euo pipefail
task_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)
cd -- "$task_root"
if [[ $# -eq 0 ]]; then
    echo 'Usage: scripts/build-native-image.sh IMAGE_TAG [additional docker build options]' >&2
    exit 2
fi
task_tag=$1
shift
[[ $(git rev-parse --show-toplevel) == "$task_root" ]] || exit 1
if [[ -n $(git status --porcelain --untracked-files=all) ]]; then
    echo 'Commit source changes before building a release image.' >&2
    exit 1
fi
task_revision=$(git rev-parse --verify HEAD)
task_base=$(awk '/^\[package\]/{p=1;next} /^\[/{p=0} p && /^version[[:space:]]*=/{gsub(/"/, ""); print $3; exit}' Cargo.toml)
[[ $task_revision =~ ^[0-9a-f]{40}$ && $task_base =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || exit 1
task_version="${task_base}+local.${task_revision:0:12}"
task_options=()
if [[ -n ${NATIVE_BUILDER_IMAGE:-} ]]; then
    task_options+=(--build-arg "BUILDER_IMAGE=$NATIVE_BUILDER_IMAGE")
fi
docker build "$@" "${task_options[@]}" \
    --build-arg "BUILD_VERSION=$task_version" \
    --build-arg "SOURCE_REVISION=$task_revision" \
    --tag "$task_tag" .
docker image inspect --format '{{.Id}} {{.Architecture}} {{json .Config.Labels}}' "$task_tag"
docker run --rm "$task_tag" --version
