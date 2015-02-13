#!/usr/bin/env python

import subprocess

def symaddr(name, binary):
	grep = subprocess.Popen(['grep', name], stdin=subprocess.PIPE,
	    stdout=subprocess.PIPE)
	nm = subprocess.Popen(['nm', '-C', binary], stdout=grep.stdin)
	nm.communicate()
	sym, err = grep.communicate()
	if err != None:
		raise ValueError('failure %s' % (err))
	sym = int(sym.strip().split()[0], 16)
	return sym

def xg(a, ins):
	print '\t'*ins + 'x/gx %s' % (hex(a))

def threadstatus(tcount, sizeof):
	print 'thread status'
	taddr = symaddr('threads', 'main.gobin')
	st_offset = (16+7)*8
	for i in range(tcount):
		xg(taddr + i*sizeof + st_offset, 1)

def threadpids(tcount, sizeof):
	print 'thread pids'
	taddr = symaddr('threads', 'main.gobin')
	st_offset = (16+7)*8+4*8
	for i in range(tcount):
		xg(taddr + i*sizeof + st_offset, 1)

def lockstatus():
	print 'locks'
	for l in ['klock', 'threadlock']:
		print '\t', l
		xg(symaddr(l, 'main.gobin'), 2)

threadstatus(64, 0xe8)
threadpids(64, 0xe8)
lockstatus()
