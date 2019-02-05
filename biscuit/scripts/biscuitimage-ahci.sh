#!/bin/sh

P=31338

echo
echo "Waiting for AHCI image on port ${P}.."
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

TFILE=$(mktemp -t)
DISK=/dev/rsd0c

l=$(disklabel ${DISK} |grep '^label:')
case $l in
	*Samsung\ SSD\ 850\ )
		;;
	*)
		echo unexpected disk label
		exit
		;;
esac

while [ 1 ]; do
	I=0
	until nc -l ${P} > $TFILE; do
		echo $I failed, retry in one sec;
		sleep 1;
		I=$(( $I + 1));
	done

	cksum -a sha1 $TFILE
	dd if=$TFILE of=$DISK bs=1m && { echo wrote disk; rboot; }
	echo disk image failed
done
