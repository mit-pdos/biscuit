#!/usr/bin/env python2
# vim: expandtab ts=4 sw=4

import subprocess

# cmds is a list of lists of commands
def piper(cmds):
    last = subprocess.Popen(cmds[0], stdout=subprocess.PIPE, bufsize=4096)
    for c in cmds[1:]:
        nc = subprocess.Popen(c, stdin=last.stdout, stdout=subprocess.PIPE, bufsize=4096)
        last.stdout.close()
        last = nc
    sout, serr = last.communicate()
    return sout, serr

class Files(object):
    def __init__(self):
        self.f = {}

    def linesfor(self, fn):
        if fn in self.f:
            return self.f[fn]
        with open(fn, 'r') as fl:
            lines = fl.readlines()
        self.f[fn] = lines
        return lines

class Sym(object):
    def __init__(self, name, s, e):
        self.name, self.start, self.end = name, s, e

    # returns true if v lies within the symbol's addresses
    def within(self, v):
        return not (v < self.start or v >= self.end)

def symlookup(fn, sym):
    c1 = ['nm', '-C', fn]
    c2 = ['sort']
    c3 = ['fgrep', '-A10', '-w', sym]
    out, _ = piper([c1, c2, c3])
    lines = filter(None, [l.strip() for l in out.split('\n')])
    if len(lines) == 0:
        print 'for', sym
        raise KeyError('no such sym')
    start = int(lines[0].split()[0], 16)
    end = 0
    found = False
    #for x in lines:
    #    print x
    for l in lines[1:]:
        end = int(l.split()[0], 16)
        if end > start:
            found = True
            break
    if not found:
        raise ValueError("fixme: couldn't find end")
    return Sym(sym, start, end)

if __name__ == '__main__':
    a, _ = piper([['printf', 'hello\nworld\nvery good\n'],
            ['tail', '-n2'],
            ['grep', 'goo'],
    ])
    if a.strip() != 'very good':
        raise 'bad'
    print 'gut'
