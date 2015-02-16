#!/usr/bin/env python

import subprocess

import elfutil

def xg(a, ins):
	print '\t'*ins + 'x/gx %s' % (hex(a))

def threadstatus(tcount, sizeof):
	print 'thread status'
	taddr = elfutil.symvaddr('main.gobin', 'threads')
	st_offset = (16+7)*8
	for i in range(tcount):
		xg(taddr + i*sizeof + st_offset, 1)

def threadpids(tcount, sizeof):
	print 'thread pids'
	taddr = elfutil.symvaddr('main.gobin', 'threads')
	st_offset = (16+7)*8+4*8
	for i in range(tcount):
		xg(taddr + i*sizeof + st_offset, 1)

def lockstatus():
	print 'locks'
	for l in ['klock', 'threadlock']:
		print '\t', l
		xg(elfutil.symvaddr('main.gobin', l), 2)

threadstatus(64, 0xe8)
threadpids(64, 0xe8)
lockstatus()
