#!/bin/sh
set -e

PUID=${PUID:-1000}
PGID=${PGID:-1000}

# Align appuser/appgroup with the host-provided PUID/PGID so files written to
# bind-mounted volumes (the media library and the database) are owned by the
# expected host user. -o permits non-unique ids in case of collision.
groupmod -o -g "$PGID" appgroup
usermod  -o -u "$PUID" -g "$PGID" appuser

# Apply timezone if provided. Go honors $TZ directly; this also fixes /etc/localtime
# for any tooling that reads it.
if [ -n "$TZ" ] && [ -f "/usr/share/zoneinfo/$TZ" ]; then
    cp "/usr/share/zoneinfo/$TZ" /etc/localtime
    echo "$TZ" > /etc/timezone
fi

# Only the database directory needs ownership fixups. Never recursively chown the
# (potentially huge) mounted media library — its files should already be owned by
# the host user matching PUID/PGID.
chown -R appuser:appgroup /app/data

exec su-exec appuser:appgroup /app/subtitle-fetcher "$@"
