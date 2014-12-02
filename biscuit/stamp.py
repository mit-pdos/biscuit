#!/usr/bin/env python

import os

fn = 'boot.bin'
sz = os.path.getsize(fn)
# both bootmain.c and boot.S also need to know the size of the bootloader in
# blocks (BOOTBLOCKS)
numblocks = 4
left = numblocks*512 - sz
if left < 0:
    raise ValueError('boot sector is bigger than numblocks')

with open(fn, 'a') as f:
    f.write(''.join([chr(0) for i in range(left)]))

with open(fn, 'r') as f:
    d = f.read(512)

if ord(d[510]) != 0x55 or ord(d[511]) != 0xaa:
    raise ValueError('sig is wrong! fix damn text ordering somehow')
