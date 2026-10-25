#!/bin/sh
# Tern in development: this checkout's code against this checkout's own data/
# (TERN_HOME=$PWD). The installed app keeps its library in
# ~/Library/Application Support/Tern, so the two never touch each other. The
# development icon comes from the code itself (see useDockIcon), not from here.
# Extra arguments go straight to the app.
#
#   ./dev.sh                # desktop + reading server on 127.0.0.1:8787
#   ./dev.sh -port 8788     # a second copy next to the installed app
#   ./dev.sh --headless     # server only, for the phone
#
# By hand this is: TERN_HOME=. go run .
set -eu
cd "$(dirname "$0")"

TERN_HOME=$PWD
export TERN_HOME

exec go run . "$@"
