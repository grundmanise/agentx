#!/bin/sh
# The container's command: builds the home from the preset PRESET names, then serves the app on it.
set -eu
mkdir -p /tmp/out
node apps/desktop/scripts/preset.ts "${PRESET:-typical}"
cd apps/desktop
exec node scripts/serve.ts preview
