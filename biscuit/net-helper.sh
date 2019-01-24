#!/bin/sh
FN="$1"
HOST="$2"
echo
echo copy to "$HOST"
sha1sum "$FN"
ls -alh "$FN"
until nc -v "$HOST" 31338 -q1 < "$FN"; do
	echo timed out. trying again...;
	sleep 1;
done
echo
echo copied Biscuit image to $HOST
echo
