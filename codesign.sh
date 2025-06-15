#!/bin/sh

set -e

SIGNING_IDENTITY="${1}"
DIRECTORY="${2}"

if [ -z "${SIGNING_IDENTITY}" ]; then
  echo "Error: No signing identity provided" >&2
  exit 1
fi

if [ -z "${DIRECTORY}" ]; then
  echo "Error: No directory provided" >&2
  exit 1
fi

find "${DIRECTORY}" -type f -perm -u+x -exec codesign --force --sign "${SIGNING_IDENTITY}" {} \;
