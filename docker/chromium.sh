#!/bin/sh
# Chromium runs as the unprivileged container user; Docker provides isolation.
exec /usr/bin/chromium --no-sandbox "$@"
