#!/usr/bin/env python2

import os
import subprocess
import sys

def usage():
	print >> sys.stderr, 'usage: %s <in gobin> <out gobin>' % (sys.argv[0])
	sys.exit(1)

def _fsrex(data):
	# FS seg override?
	c = ord(data[0])
	if c != 0x64:
		return False
	c = ord(data[1])
	# 64 bit width, 64bit width + r8-r15 reg
	rexes = [0x48, 0x4c]
	if c not in rexes:
		return False
	return True

def _movs(data):
	# rewrite all movs using an fs segment override to use a gs segment
	# override
	if not _fsrex(data):
		return False

	c = ord(data[2])
	# moves that i don't expect but may see someday
	maybemov = [0x88, 0x8c, 0x8e, 0xa0, 0xa1, 0xa2, 0xa3, 0xb0, 0xb8, 0xc6]
	if c in maybemov:
		raise ValueError('new kind of mov %x' % (c))

	movs = [0x8b, 0x89, 0xc7]
	if c not in movs:
		return False
	return True

def _cmps(data):
	# rewrite all cmps using an fs segment override to use a gs segment
	# override
	if not _fsrex(data):
		return False
	if data[2] != '\x39':
		return False
	return True

def rewrite(f):
	spots = getspots(f.name)

	data = list(f.read())
	found = 0
	for i in spots:
		d = data[i:i+3]
		if _movs(d) or _cmps(d):
			found += 1
			data[i] = '\x65'
	if found == 0:
		raise ValueError('didnt find a single occurance')
	skipped = len(spots) - found
	print >> sys.stderr, 'patched %d instructions (%d skipped)' % (found, skipped)
	if found < skipped:
		raise ValueError('more skipped than found')
	return ''.join(data)

def getspots(fn):
	# find all potential file offsets where an instruction uses %fs
	odcmd = ['objdump', '--prefix-addresses', '-F', '-d', fn]
	od = subprocess.Popen(odcmd, stdout=subprocess.PIPE)
	gcmd = ['grep', '%fs']
	gr = subprocess.Popen(gcmd, stdin=od.stdout, stdout=subprocess.PIPE)

	od.stdout.close()
	data, _ = gr.communicate()

	ret = []
	for l in data.split('\n'):
		l = l.strip()
		if l == '':
			continue
		l = l.split('File Offset: ')
		if len(l) != 2:
			print l
			raise ValueError('unexpected output')
		l = l[1].split(')')[0]
		ret.append(int(l, 16))
	return ret

if len(sys.argv) != 3:
	usage()

fn = sys.argv[1]
dn = sys.argv[2]

newdata = ''
with open(fn, 'r+b') as f:
	newdata = rewrite(f)

with open(dn, 'w') as nf:
	nf.write(newdata)

print >> sys.stderr, 'created %s' % (dn)
