#!/usr/bin/env python2

import getopt
import subprocess
import sys

def usage():
	print >> sys.stderr
	print >> sys.stderr, 'usage: %s <redis PMU profile>' % (sys.argv[0])
	print >> sys.stderr
	sys.exit(-1)

def openrips(fn):
	f = open(fn)
	rips = []
	for l in f.readlines():
		l = l.strip()
		if l == '':
			continue
		l = l.split()
		rip = l[0]
		times = int(l[2])
		for i in range(times):
			rips.append(rip)
	f.close()
	return rips

def divrips(rips):
	ur = []
	kr = []
	for r in rips:
		if r.find('2c8') != -1:
			ur.append(r)
		else:
			kr.append(r)
	return ur, kr

def getsmap(fn):
	cmd = ['nm', '-C', fn]
	nm = subprocess.Popen(cmd, stdout=subprocess.PIPE)
	scmd = ['sort']
	sort = subprocess.Popen(scmd, stdin=nm.stdout,
	    stdout=subprocess.PIPE)
	nm.stdout.close()
	out, _ = sort.communicate()

	ret = []
	for l in out.split('\n'):
		l = l.strip()
		if l == '':
			continue
		l = l.split()
		if len(l) != 3:
			continue
		ret.append(l)
	return ret

# list where each element is tuple of (symbol, start, end)
def getsmap2(binfn):
	smap = getsmap(binfn)
	ret = []
	for i, s in enumerate(smap):
		r1 = s[2]
		r2 = int(s[0], 16)
		r3 = 0x7fffffffffffffff
		if i != len(smap) - 1:
			r3 = int(smap[i+1][0], 16)
		ret.append((r1,r2,r3))
	return ret

def rip2func(rips, binfn, smap):
	ipbyname = {}

	si = 0
	for _kr in rips:
		kr = int(_kr, 16)
		found = False
		while True:
			s = smap[si]
			n = s[0]
			low = s[1]
			hi = s[2]
			if kr >= low and kr < hi:
				if n not in ipbyname:
					ipbyname[n] = []
				ipbyname[n].append(kr)
				found = True
				break
			si += 1
		if not found:
			raise ValueError("didn't find rip %s" % (_kr))
	fin = []
	for f in ipbyname:
		fin.append((len(ipbyname[f]), f))
	fin.sort()
	fin.reverse()
	return fin, ipbyname

def disass(fname, rips, smap, binfn):
	found = False
	start = 0
	end = 0
	for s in smap:
		if s[0] == fname:
			found = True
			start = s[1]
			end = s[2]
			break
	if not found:
		raise ValueError("didn't find func")

	odcmd = ['objdump', '-d', '--start-address=%#x' % (start),
	    '--stop-address=%#x' % (end), '--no-show-raw-insn', binfn]
	od = subprocess.Popen(odcmd, stdout=subprocess.PIPE)
	text, _ = od.communicate()
	ret = []
	for l in text.split('\n'):
		l = l.strip()
		if l == '':
			continue
		if l.find('file format') != -1:
			continue
		if l.find('Disassembly of') != -1:
			continue
		# don't try to parse ip of first line (name of function)
		if l[0] == '0':
			print l
			continue

		thisip = l.split()[0]
		thisip = int(thisip[:thisip.find(':')], 16)
		c = rips.count(thisip)
		print '%6d %s' % (c, l)

def dumpsec(secname, rips, binfn, nsamp):
	rips.sort()

	smap = getsmap2(binfn)
	fin, ipbn = rip2func(rips, binfn, smap)
	print '==== %s ====' % (secname)
	tot = 0
	for f in fin:
		n = f[1].strip()
		c = f[0]
		s = float(c)
		tot += c
		cs = '(%d)' % (c)
		print '%-35s %6.2f %6s' % (n, s/nsamp, cs)
		fname = f[1]
		if dumpips:
			disass(fname, ipbn[fname], smap, binfn)
	print '---------'
	print 'total %6.2f' % (float(tot)/nsamp)

def dump(rips, dumpips=False):
	kbin = '/opt/cody/biscuit/biscuit/main.gobin'
	#ubin = '/opt/cody/biscuit-redis/src/redis-server'
	ubin = '/opt/cody/biscuit/biscuit/user/c/sfork'

	samples = len(rips)
	urips, krips = divrips(rips)
	dumpsec('KERNEL TIME', krips, kbin, samples)
	dumpsec('USER   TIME', urips, ubin, samples)

opts, args = getopt.getopt(sys.argv[1:], 'd')
if len(args) != 1:
	usage()

dumpips = False
for o in opts:
	if o[0] == '-d':
		dumpips = True
prof = args[0]
rips = openrips(prof)

#manual(rips)

dump(rips, dumpips)
