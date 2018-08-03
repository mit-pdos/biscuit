#!/bin/sh

BPATH="/home/ccutler/biscuit/biscuit"

[ $# -ne 1 ] && { echo "usage: $0 <tool output file>"; exit 1; }
F="$1"
T=$(tty)

echo "BOUNDS"

N=$(grep -e 'MAX [SM]' -e 'LOOP BOUND' -- $F |sort -u |wc -l)
echo "total: $N"
I=0
grep -e 'MAX [SM]' -e 'LOOP BOUND' -- $F |sort -u | while read ln; do
	d=$(echo $ln |grep -o '[^[:space:]]*\.go[^[:space:]]*')
	fn=$(echo $d | cut -f1 -d:)
	lnum=$(echo $d | cut -f2 -d:)
	echo "$I/$N: $fn $lnum? [Y/n]"
	I=$(($I + 1))
	read yn < $T
	case $yn in
	"n"|"N")
		continue
		;;
	*)
		;;
	esac
	tmp=$(mktemp)
	#echo $tmp
	echo $ln > $tmp
	vim +$lnum -c ":pedit $tmp" -c ":cd $BPATH" -c ":set path+=**" $fn < $T
	rm -f $tmp
done

echo "INFINITE ALLOCS"
N=$(grep -c 'INFINITE ALLOC' -- $F)
echo "total: $N"
I=0
grep 'INFINITE ALLOC' -- $F | while read ln; do
	d=$(echo $ln |grep -o '[^[:space:]]*\.go[^[:space:]]*')
	fn=$(echo $d | cut -f1 -d:)
	lnum=$(echo $d | cut -f2 -d:)
	echo "$I/$N: $fn $lnum? [Y/n]"
	I=$(($I + 1))
	read yn < $T
	case $yn in
	"n"|"N")
		continue
		;;
	*)
		;;
	esac
	tmp=$(mktemp)
	echo $tmp
	echo $ln > $tmp
	vim +$lnum -c ":pedit $tmp" -c ":cd $BPATH" -c ":set path+=**" $fn < $T
	rm -f $tmp
done

exit 0
