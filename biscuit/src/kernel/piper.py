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

if __name__ == '__main__':
    a, _ = piper([['printf', 'hello\nworld\nvery good\n'],
            ['tail', '-n2'],
            ['grep', 'goo'],
    ])
    if a.strip() != 'very good':
        raise 'bad'
    print 'gut'
