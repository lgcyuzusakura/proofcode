#!/bin/sh
set -eu

chown proofcode:proofcode /workspaces
exec su-exec proofcode proofcode-runner "$@"
