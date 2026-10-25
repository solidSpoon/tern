#!/bin/sh
# Tern in development: this checkout's code against this checkout's own data/
# (TERN_HOME=$PWD). The installed app keeps its library in
# ~/Library/Application Support/Tern, so the two never touch each other. The
# development icon comes from the code itself (see useDockIcon), not from here.
# Extra arguments go straight to the app.
#
# Development listens on 8788, not the 8787 of the installed app, so the two
# can run side by side. PORT overrides it; a port saved in the dev Settings
# wins over both.
#
#   ./dev.sh                # desktop + reading server on 127.0.0.1:8788
#   ./dev.sh --headless     # server only, for the phone
#
# By hand this is: TERN_HOME=. PORT=8788 go run .
set -eu
cd "$(dirname "$0")"

TERN_HOME=$PWD
PORT=${PORT:-8788}
export TERN_HOME PORT

exec go run . "$@"
