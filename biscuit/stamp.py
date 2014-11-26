#!/usr/bin/env python

import os

fn = 'boot.bin'
sz = os.path.getsize(fn)
left = 512 - sz
if left < 2:
    raise ValueError('not enough room for boot sig?')

with open(fn, 'a') as f:
    f.write(''.join([chr(0) for i in range(left - 2)] + [chr(0x55), chr(0xaa)]))
