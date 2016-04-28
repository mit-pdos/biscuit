#!/bin/sh
objdump -d main.gobin |grep -e '^000.*<.*>:[[:space:]]*$' -e '.*[[:space:]]callq[[:space:]]'  |less
