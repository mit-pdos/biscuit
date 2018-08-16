#!/bin/sh -eu

R="/home/ccutler/biscuit/biscuit/src/kernel/"
WBINST="$R/wbinst.py"

[ $# -ne 2 ] && { echo "usage: $0 <profile> <kernel>"; exit 1; }
P="$1"
K="$2"
[ ! -r $P ] && { echo "cannot read $P"; exit 1; }
[ ! -r $K ] && { echo "cannot read $K"; exit 1; }

gen() {
	$WBINST $K || { echo "$WBINST $K failed"; exit 1; }

	for f in *.rips; do
		out=$(basename $f .rips)
		out="$out.matches"
		[ -s $out ] && { echo "skipping $out..."; continue; }
		echo "$f -> $out"
		{ while read r; do grep "$r" "$P" || true; done } < $f > $out
	done
}

for f in bounds div nilptrs splits types wbars; do
	[ ! -s $f.rips ] && { gen; break; }
done

nsamp=$(awk '{print $3}' $P |paste -s -d+ |bc)

# arg 1 is name of tax, the rest of files to sum
sum() {
	local _n="$1"
	shift
	echo "**** $_n *****"
	tot=$(cat $* |awk '{print $3}' | paste -s -d+ |bc)
	frac=$(echo "$tot / $nsamp" | bc -l |grep -o '^[^.]*\.....')
	echo "TOTAL: $frac ($tot / $nsamp)"
}

sum "PROLOGUES (without newstack! see backtrace)" splits.matches
sum "WRITE BARRIERS (without findObject! see backtrace)" wbars.matches
sum "SAFETY" bounds.matches div.matches nilptrs.matches types.matches
