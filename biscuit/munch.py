#!/usr/bin/env python2

import subprocess
import sys

def usage():
	print >> sys.stderr
	print >> sys.stderr, 'usage: %s <redis PMU profile>' % (sys.argv[0])
	print >> sys.stderr

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

def linemap(rips, fn):
	acmd = ['addr2line', '-e', fn]
	a2l = subprocess.Popen(acmd, stdin=subprocess.PIPE,
	    stdout=subprocess.PIPE)

	d = [x.split()[0] for x in rips]
	d = '\n'.join(d)
	out, _ = a2l.communicate(d)
	return out.split('\n')

def litclines(ul):
	ret = 0
	for l in ul:
		if l.find('litc.c') != -1:
			ret += 1
	return ret

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

def smaprange(smap, name):
	for i, s in enumerate(smap):
		if s[2] == name:
			ret1 = int(s[0], 16)
			ret2 = '0x7fffffffffffffff'
			if i != len(smap) - 1:
				ret2 = int(smap[i+1][0], 16)
			return s[2], ret1, ret2
	raise KeyError('no such name %s' % (name))

def rangecount(rips, smap, name):
	ret = 0
	fn, low, hi = smaprange(smap, name)
	for r in rips:
		ip = int(r, 16)
		if ip >= low and ip < hi:
			ret += 1
	return ret

def rangecountlist(rips, smap, names):
	ret = 0
	for i in names:
		ret += rangecount(rips, smap, i)
	return ret

def rangesloppy(rips, smap, name):
	ret = 0
	for r in rips:
		for i, s in enumerate(smap):
			if s[2].find(name) == -1:
				continue
			low = int(s[0], 16)
			hi = '0x7fffffffffffffff'
			if i != len(smap) - 1:
				hi = int(smap[i+1][0], 16)
			ip = int(r, 16)
			if ip >= low and ip < hi:
				ret += 1
				break
	return ret

def rangesloppylist(rips, smap, names):
	ret = 0
	for i in names:
		ret += rangesloppy(rips, smap, i)
	return ret

class Counter(object):
	def __init__(self, rips, smap):
		self.rips, self.smap = rips, smap

	def cnt(self, names):
		return rangecountlist(self.rips, self.smap, names)

	def slop(self, names):
		return rangesloppylist(self.rips, self.smap, names)

class Printer(object):
	def __init__(self, samples):
		self.t = samples
		self.totals = [0 for i in range(10)]

	def pr(self, name, level, samps):
		pct = float(samps)/self.t
		self.totals[level] += pct
		print '    '*level + '%-20s %.2f' % (name, pct)
	def prtot(self, level):
		tot = self.totals[level]
		self.totals[level] = 0
		print '    '*level + '----------------'
		print '    '*level + '%-20s %.2f' % ('total', tot)

kbin = '/opt/cody/biscuit/biscuit/main.gobin'
ubin = '/opt/cody/biscuit-redis/src/redis-server'

def manual(rips):

	samples = len(rips)
	urips, krips = divrips(rips)

	pr = Printer(samples)

	print
	print '==== PMU PROFILE ===='
	print
	print '%d samples' % (samples)
	pr.pr('kernel', 0, len(krips))
	pr.pr('user', 0, len(urips))

	print
	print '==== KERNEL TIME ===='

	ksmap = getsmap(kbin)
	#kl = linemap(krips, kbin)

	t = Counter(krips, ksmap)

	print 'main'
	l = [ 'main.' + i for i in ['sys_poll', '_checkfds']]
	pr.pr('poll', 1, t.cnt(l))

	l = [ 'main.' + i for i in ['sys_read', 'sys_write']]
	pr.pr('read/write', 1, t.cnt(l))

	l = ['main.' + i for i in ['(*proc_t).mkuserbuf', '(*pipe_t).pipe_start']]
	pr.pr('pr allocs', 1, t.cnt(l))

	l = ['main.' + i for i in ['readn', 'writen']]
	pr.pr('stupid', 1, t.cnt(l))

	pr.prtot(1)

	print 'runtime'
	l = [ 'runtime.' + i for i in ['Rdmsr', 'Wrmsr']]
	pr.pr('msr', 1, t.cnt(l))

	l = [ 'runtime.' + i for i in ['Userrun', '_Userrun']]
	pr.pr('userrun', 1, t.cnt(l))

	l = [ 'runtime.' + i for i in ['memmove', 'typedmemmove']]
	l += [ 'reflect.' + i for i in ['typedmemmove', 'typedmemmovepartial']]
	pr.pr('mem', 1, t.cnt(l))

	l = [ 'runtime.' + i for i in ['timerproc']]
	pr.pr('timers', 1, t.cnt(l))

	l = [ 'runtime.hack_' + i for i in [ 'clone', 'exit', 'futex', 'mmap',
	    'munmap', 'nanotime', 'setitimer', 'sigaltstack', 'syscall', 'usleep',
	    'write']]
	pr.pr('hack_*', 1, t.cnt(l))

	l = [ 'runtime.' + i for i in ['scanblock', 'greyobject', 'mallocgc',
	    'gcAssistAlloc', 'heapBitsSweepSpan', 'largeAlloc', 'newobject',
	    'profilealloc']]
	pr.pr('GC', 1, t.cnt(l))
	for i in l:
		n = i[8:]
		pr.pr(n, 2, t.cnt([i]))
	pr.prtot(2)

	l = [ 'runtime.' + i for i in ['callwritebarrier', 'writebarrierptr',
	    'writebarrierptr_nostore', 'writebarrierptr_nostore1']]
	pr.pr('write barriers', 1, t.cnt(l))

	pr.prtot(1)

	print
	print '==== USER TIME ===='

	usmap = getsmap(ubin)

	t = Counter(urips, usmap)

	ul = linemap(urips, ubin)
	llitc = litclines(ul)

	print
	pr.pr('litc', 0, llitc)

	l = ['_malloc', 'malloc', 'calloc', '_findseg', '_free', 'free', 'realloc']
	pr.pr('malloc', 1, t.cnt(l))

	l = ['memcmp', 'memcpy', 'memmove', 'memset']
	pr.pr('mem', 1, t.cnt(l))

	l = ['poll', 'select']
	pr.pr('poll', 1, t.cnt(l))

	l = ['gettimeofday']
	pr.pr('gtod', 1, t.cnt(l))

	l = ['acquire', 'release']
	pr.pr('locks', 1, t.cnt(l))
	pr.prtot(1)

	print
	pr.pr('other', 0, len(ul) - llitc)
	print

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

def rip2func(rips, binfn):
	smap = getsmap2(binfn)
	funcs = {}

	si = 0
	for _kr in rips:
		kr = int(_kr, 16)
		while True:
			s = smap[si]
			n = s[0]
			low = s[1]
			hi = s[2]
			if kr >= low and kr < hi:
				if n not in funcs:
					funcs[n] = 0
				funcs[n] += 1
				break
			si += 1
	fin = []
	for f in funcs:
		fin.append((funcs[f], f))
	fin.sort()
	fin.reverse()
	return fin

def kdump(rips):
	samples = len(rips)
	urips, krips = divrips(rips)
	krips.sort()

	fin = rip2func(krips, kbin)
	tot = 0
	for f in fin:
		n = f[1].strip()
		s = float(f[0])
		tot += f[0]
		print '%-35s %6.2f' % (n, s/samples)
	print '---------'
	print 'total %6.2f' % (float(tot)/samples)

if len(sys.argv) != 2:
	usage()
prof = sys.argv[1]
rips = openrips(prof)

manual(rips)
kdump(rips)
