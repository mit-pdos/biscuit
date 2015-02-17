#!/usr/bin/env python

# this script takes as input the filenames of the boot and kernel images and
# creates an empty disk, poking the block number of the first free block into
# the kernel image.

import os
import sys

import elfutil

blocksz = 512
hdsize = 20 * 1024 * 1024

def roundup(n, to):
	ret = n + to - 1
	return ret - (ret % to)

def fblocks(fn):
	s = os.stat(fn)
	return roundup(s.st_size, blocksz) / blocksz

def poke(data, where, num):
	data[where+0] = chr((num >> 0*8) & 0xff)
	data[where+1] = chr((num >> 1*8) & 0xff)
	data[where+2] = chr((num >> 2*8) & 0xff)
	data[where+3] = chr((num >> 3*8) & 0xff)
	data[where+4] = chr((num >> 4*8) & 0xff)
	data[where+5] = chr((num >> 5*8) & 0xff)
	data[where+6] = chr((num >> 6*8) & 0xff)
	data[where+7] = chr((num >> 7*8) & 0xff)

def le8(num):
	l = [chr((num >> i*8) & 0xff) for i in range(8)]
	return ''.join(l)

if len(sys.argv) != 4:
	print >> sys.stderr, 'usage: %s <boot image> <kernel image> <output image>' % (sys.argv[0])
	sys.exit(-1)

bfn = sys.argv[1]
kfn = sys.argv[2]
ofn = sys.argv[3]

hdblocks = roundup(hdsize, blocksz) / blocksz
usedblocks = fblocks(bfn)
usedblocks += fblocks(kfn)
remaining = hdblocks - usedblocks

where = elfutil.fileoffsetofsym(kfn, 'fsblock_start', 'D')
print >> sys.stderr, 'free blocks start at %#x' % (usedblocks)
print >> sys.stderr, 'symbol fsblock_start at %#x' % (where)

with open(bfn, 'r') as bf, open(kfn, 'r') as kf, open(ofn, 'w') as of:
	kfdata = list(kf.read())
	poke(kfdata, where, usedblocks)

	of.write(bf.read())
	of.write(''.join(kfdata))
	remaining -= 1
	if remaining < 0:
		raise 'ruh roh'
	# pad out kernel image to block
	of.write('\0'*(blocksz - (len(kfdata) % blocksz)))

	# superblock fields: freeblock start, freeblock length, log length, and
	# last block
	of.write(le8(usedblocks + 1))
	of.write(le8(10))
	of.write(le8(30))
	# skip root inode
	of.write(le8(0))
	of.write(le8(hdblocks))
	of.write('\0'*(blocksz - 5*8))
	for i in range(remaining):
		of.write('\0'*512)
print >> sys.stderr, 'created "%s" of length %d blocks' % (ofn, hdblocks)
print >> sys.stderr, '(fs starts at %#x in "%s")' % (usedblocks*blocksz, ofn)
