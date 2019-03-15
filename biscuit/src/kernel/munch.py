#!/usr/bin/env python2
# vim: expandtab ts=4 sw=4

import getopt
import subprocess
import sys

def usage():
    print >> sys.stderr
    print >> sys.stderr, 'usage: %s [-db] <PMU profile> <kernel binary> <user binary>' % (sys.argv[0])
    print >> sys.stderr
    sys.exit(-1)

# returns whether line is a backtrace sentinel value, the ID of the CPU
# which took the sample (only valid if first return is true), and whether the
# stack unwind failed.
def isbtsent(line):
    btsents = [0xdeadbeefdead0000, 0xfeedfacefeed0000]
    sm = 0xffffffffffff0000
    cpuidm = ~sm
    v = int(line.split()[0], 16)
    t = v & sm
    if t in btsents:
        cpuid = v & cpuidm
        unwfailed = t == btsents[1]
        return True, cpuid, unwfailed
    return False, 0, True

def openrips(fn):
    f = open(fn)
    lines = f.readlines()
    lines = filter(None, [x.strip() for x in lines])

    #remnext = False
    #newl = []
    #for l in lines:
    #    if l == btsents[1]:
    #        remnext = True
    #    elif remnext:
    #        remnext = False
    #    else:
    #        newl.append(l)
    #lines = newl

    backtrace = False
    btfailed = 0
    for l in lines:
        btsent, _, unwindfailed = isbtsent(l)
        if btsent:
            backtrace = True
            if unwindfailed:
                btfailed += 1
    # map of CPU ID -> lists of sample IPs (without the return address IPs in a
    # backtrace)
    cpurips = {}
    # map of CPU ID -> list of lists of sample IP with return addresses
    cpubts = {}
    for i in range(40):
        cpurips[i] = []
        cpubts[i] = []
    newbt = []
    ripnext = False
    cpunext = -1
    for l in lines:
        if backtrace:
            btsent, cpuid, _ = isbtsent(l)
            if btsent:
                if len(newbt) > 0:
                    bts = cpubts[cpunext]
                    bts.append(newbt)
                ripnext = True
                cpunext = cpuid
                newbt = []
            else:
                newbt.append(l)
                if ripnext:
                    ripnext = False
                    crips = cpurips[cpuid]
                    crips.append(l)
        else:
            l = l.split()
            rip = l[0]
            times = int(l[2])
            cpuid = int(rip, 16) >> 56
            rip = int(rip, 16) & 0x00ffffffffffffff
            rip = '%x' % (rip)
            while len(rip) != 16:
                rip = '0' + rip
            crips = cpurips[cpuid]
            for i in range(times):
                crips.append(rip)
    f.close()
    allbts = sum([sum([len(x) for x in y]) for y in cpubts.values()])
    if backtrace and btfailed != 0:
        fp = float(btfailed) / allbts * 100
        print 'backtrace failed for %d%% (%d / %d)\n' % (fp, btfailed, allbts)
    return cpurips, cpubts

def isuser(r):
    return r.startswith('00002c8')

def divrips(rips):
    ur = []
    kr = []
    for r in rips:
        if isuser(r):
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
# consider using "addr2line -f" instead
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

# rips must be sorted in ascending order
def rip2func(rips, smap):
    # dict mapping symbol name -> list of rips in that symbol's range
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
    # list of tuples (number of rips in symbol, symbolname)
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
    fin, ipbn = rip2func(rips, smap)
    print '==== %s ====' % (secname)
    cum = 0.0
    tot = 0
    for f in fin:
        n = f[1].strip()
        c = f[0]
        s = float(c)
        tot += c
        cs = '(%d)' % (c)
        frac = s/nsamp
        cum += s
        print '%-35s %6.4f %6s (%6.4f)' % (n, frac, cs, cum/nsamp)
        fname = f[1]
        if dumpips:
            disass(fname, ipbn[fname], smap, binfn)
    print '---------'
    if nsamp == 0:
        print 'total 0%'
    else:
        print 'total %6.2f' % (float(tot)/nsamp)

def dump(kbin, ubin, rips, dumpips=False):
    samples = len(rips)
    urips, krips = divrips(rips)
    dumpsec('KERNEL TIME', krips, kbin, samples)
    dumpsec('USER     TIME', urips, ubin, samples)

class gnode(object):
    def __init__(self, name):
        self.name = name
        self.cees = {}
        # samps is the count of the number of samples which occured in this
        # function or any of this function's callees.
        self.samps = 0
        self.frac = 0.0

    def called(self, cnode):
        if cnode not in self.cees:
            self.cees[cnode] = 0
        old = self.cees[cnode]
        self.cees[cnode] = old + 1

    # returns (callee node, times called by this caller)
    def callees(self):
        return self.cees.items()

class graph(object):
    def __init__(self, rip2syms):
        self._nodes = {}
        self.rip2syms = rip2syms

    def nodebyrip(self, rip):
        # no user backtraces
        if isuser(rip):
            return self.ensurenode('USER')
        rip = int(rip, 16)
        return self.ensurenode(self.rip2syms[rip])

    def ensurenode(self, name):
        if name in self._nodes:
            return self._nodes[name]
        ret = gnode(name)
        self._nodes[name] = ret
        return ret

    def nodes(self):
        return self._nodes.values()

def callers(binfn, bts, builddot):
    smap = getsmap2(binfn)
    rips = []
    for bt in bts:
        rips += bt
    rips.sort()
    _, krips = divrips(rips)
    _, func2ips = rip2func(krips, smap)
    rip2syms = {}
    for func, frips in func2ips.items():
        for rip in frips:
            rip2syms[rip] = func
    # map of function names to graph nodes
    g = graph(rip2syms)
    for bt in bts:
        # add (cumulative) sample counts
        for i in range(len(bt)):
            node = g.nodebyrip(bt[i])
            node.samps += 1

        # add all callee edges
        for i in range(len(bt) - 1):
            callee = g.nodebyrip(bt[i])
            caller = g.nodebyrip(bt[i + 1])
            caller.called(callee)
    samps = len(bts)
    maxfrac = 0
    for n in g.nodes():
        n.frac = float(n.samps) / samps
        if n.frac > maxfrac:
            maxfrac = n.frac

    if builddot:
        maxnode = 4
        minnode = 1
        ndelta = maxnode - minnode
        with open('graph.dot', 'w') as f:
            f.write('digraph {\n')
            for n in g.nodes():
                s = minnode + ndelta * (n.frac / maxfrac)
                lab = '\"\N\\n%.2f%%\"' % (n.frac * 100)
                f.write('\t\"%s\" [height=%.2f, width=%.2f, label=%s]\n' % (n.name, s, s, lab))
            for n in g.nodes():
                for c, _ in n.callees():
                    f.write('\t\"%s\" -> \"%s\" [penwidth=1]\n' % (n.name, c.name))
            f.write('}\n')

    topc = sorted(g.nodes(), key = lambda x:x.samps, reverse=True)
    topc = filter(lambda x: x.frac > 0.01, topc)
    print '==== TOP CALLERS ===='
    for x in topc:
        n = x.name
        frac = float(x.samps)/samps
        cs = '(%d)' % (x.samps)
        print '%-35s %6.4f %6s' % (n, frac, cs)
    print '==== CALEES ===='
    print
    for x in topc:
        n = x.name
        cs = '(%d)' % (x.samps)
        print '%-35s %6.4f %6s' % (n, x.frac, cs)
        cees = sorted(x.callees(), key=lambda x:x[0].frac * float(x[1])/x[0].samps, reverse=True)
        for c, times in cees:
            cs = '(%d)' % (times)
            fromme = float(times)/c.samps
            print '\t%-35s %6.4f %6s' % (c.name, c.frac * fromme, cs)
    print

opts, args = getopt.getopt(sys.argv[1:], 'db')
if len(args) != 3:
    usage()

dumpips = False
buildg = False
for o in opts:
    if o[0] == '-d':
        dumpips = True
    elif o[0] == '-b':
        buildg = True
prof = args[0]
kbin = args[1]
ubin = args[2]
crips, cbts = openrips(prof)

for cpuid, bts in cbts.items():
    if len(bts) == 0:
        continue
    print
    print '===== CPU %d BACKTRACE =======' % (cpuid)
    print
    callers(kbin, bts, buildg)

for cpuid, rips in crips.items():
    if len(rips) == 0:
        continue
    print
    print '===== CPU %d =======' % (cpuid)
    print
    dump(kbin, ubin, rips, dumpips)
