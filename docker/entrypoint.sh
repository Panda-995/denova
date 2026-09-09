#!/bin/sh
set -eu
/opt/denova/denova-container-init
exec "$@"
