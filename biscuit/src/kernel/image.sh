#!/bin/sh

echo
echo "Doing biscuit stuff..."
echo

TFILE=/tmp/wtf.img
DISK=/dev/wd0c
while [ 1 ]; do
	I=0
	rm -f $TFILE
	until nc -v mat.csail.mit.edu 31337 > $TFILE; do
		echo $I failed, retry in one sec;
		sleep 1;
		I=$(( $I + 1));
	done

	cksum -a sha1 $TFILE
	dd if=$TFILE of=$DISK
	echo wrote disk
	echo "reboot?"
	read R
	[ $R == "y" -o $R == "y" ] && reboot
done
