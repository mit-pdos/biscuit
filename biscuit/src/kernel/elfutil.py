#!/usr/bin/env python

import subprocess

class Seg:
	def __init__(self, offset, va, filesz):
		self.offset, self.va, self.filesz = offset, va, filesz
	def contains(self, va):
		if self.va <= va and self.va + self.filesz >= va:
			return True
		return False
class Pheads:
	def __init__(self):
		self.segs = []
	def add(self, offset, va, filesz):
		self.segs.append(Seg(offset, va, filesz))
	def fileoffset(self, va):
		for s in self.segs:
			if s.contains(va):
				return s.offset + va - s.va
		raise ValueError('va %#x not found' % (va))

def getpheads(fn):
	grep = subprocess.Popen(['grep', '-A1', 'LOAD'], stdin=subprocess.PIPE,
	    stdout=subprocess.PIPE)
	relf = subprocess.Popen(['readelf', '-l', fn], stdout=grep.stdin)
	relf.communicate()
	data, err = grep.communicate()
	if err != None:
		raise ValueError('failure %s' % (err))
	data = [i.strip() for i in data.strip().split('\n')]
	if (len(data) % 2) != 0:
		raise ValueError('data len isnt even?')

	ret = Pheads()
	for i in range(len(data)/2):
		idx = 2*i
		fields = data[idx].split()
		off = int(fields[1], 16)
		va = int(fields[2], 16)
		fields = data[idx+1].split()
		filesz = int(fields[0], 16)
		ret.add(off, va, filesz)
	return ret

def symvaddr(binary, name, dtype=None):
	grep = subprocess.Popen(['grep', name], stdin=subprocess.PIPE,
	    stdout=subprocess.PIPE)
	nm = subprocess.Popen(['nm', '-C', binary], stdout=grep.stdin)
	nm.communicate()
	sym, err = grep.communicate()
	if err != None:
		raise ValueError('failure %s' % (err))
	saddr = int(sym.strip().split()[0], 16)
	section = sym.strip().split()[1]
	if dtype != None and section != dtype:
		raise ValueError('data type mismatch (%s, %s)' % (section, dtype))
	return saddr

def fileoffsetofsym(fn, sym, dtype=None):
	pheads = getpheads(fn)
	saddr = symvaddr(fn, sym, dtype)
	fileoffset = pheads.fileoffset(saddr)
	return fileoffset

if __name__ == '__main__':
	sym = 'fsblock_start'
	a = fileoffsetofsym('main.gobin', sym, 'D')
	print '%s %#x %d' % (sym, a, a)
