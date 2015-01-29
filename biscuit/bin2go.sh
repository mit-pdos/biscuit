#!/bin/sh
set -e -u

[ ! $# -eq 1 ] && { echo "usage: $0 <binfile>" 1>&2; exit 1; }

F=$1
NF=`basename $F| sed "s/[^[:alnum:]]/_/g"`
[ ! -r $F ] && { echo "cannot read $F" 1>&2; exit 1; }

UTIL="xxd"
which $UTIL > /dev/null 2>&1 || { echo "cannot find $UTIL" 1>&2; exit 1; }

X=`which $UTIL`
echo "var _bin_$NF = []uint8{"
$X -i $F |tail -n+2 | sed "s/ \(0x[[:xdigit:]][[:xdigit:]]\)$/\1,/" \
	|sed "s/^unsigned int.*\(=.*\);/var _bin_${NF}_len int \1/"
