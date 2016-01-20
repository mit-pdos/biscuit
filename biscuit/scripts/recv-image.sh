#!/bin/sh

P=31337

echo
echo "Waiting for connection on port ${P}.."
echo

rboot()
{
	echo "rebooting in 5 seconds"
	for i in `jot 5`; do
		echo -n $i...
		sleep 1;
	done
	reboot
}

TFILE=/tmp/wtf.img
DISK=/dev/wd0c
while [ 1 ]; do
	I=0
	rm -f $TFILE
	until nc -w 1 -l ${P} > $TFILE; do
		echo $I failed, retry in one sec;
		sleep 1;
		I=$(( $I + 1));
	done

	cksum -a sha1 $TFILE
	dd if=$TFILE of=$DISK
	echo wrote both disks
	#echo "reboot?"
	#read R
	#[ $R == "y" -o $R == "y" ] && reboot
	rboot
done
