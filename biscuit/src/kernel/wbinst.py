#!/usr/bin/env python2
# vim: expandtab ts=4 sw=4

import subprocess

from capstone import *
from capstone.x86 import *

def opdump(x, op, c):
    if op.type == X86_OP_REG:
        print("\t\toperands[%u].type: REG = %s" % (c, x.reg_name(op.reg)))
    if op.type == X86_OP_IMM:
        print("\t\toperands[%u].type: IMM = 0x%s" % (c, op.imm))
    if op.type == X86_OP_FP:
        print("\t\toperands[%u].type: FP = %f" % (c, op.fp))
    if op.type == X86_OP_MEM:
        print("\t\toperands[%u].type: MEM" % c)
        if op.mem.segment != 0:
            print("\t\t\toperands[%u].mem.segment: REG = %s" % (c, x.reg_name(op.mem.segment)))
        if op.mem.base != 0:
            print("\t\t\toperands[%u].mem.base: REG = %s" % (c, x.reg_name(op.mem.base)))
        if op.mem.index != 0:
            print("\t\t\toperands[%u].mem.index: REG = %s" % (c, x.reg_name(op.mem.index)))
        if op.mem.scale != 1:
            print("\t\t\toperands[%u].mem.scale: %u" % (c, op.mem.scale))
        if op.mem.disp != 0:
            print("\t\t\toperands[%u].mem.disp: 0x%s" % (c, op.mem.disp))

class Sym(object):
    def __init__(self, name, s, e):
        self.name, self.start, self.end = name, s, e

    # returns true if v lies within the symbol's addresses
    def within(self, v):
        return not (v < self.start or v >= self.end)

def symlookup(fn, sym):
    c1 = ['nm', '-C', fn]
    nm = subprocess.Popen(c1, stdout=subprocess.PIPE)
    c2 = ['sort']
    sort = subprocess.Popen(c2, stdin=nm.stdout, stdout=subprocess.PIPE)
    c3 = ['grep', '-A10', '-w', sym]
    grep = subprocess.Popen(c3, stdin=sort.stdout, stdout=subprocess.PIPE)
    nm.stdout.close()
    sort.stdout.close()
    out, _ = grep.communicate()
    lines = filter(None, [l.strip() for l in out.split('\n')])
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
        raise 'no end?'
    return Sym(sym, start, end)

# returns a list of output lines
def readelfgrep(fn, rf, gre):
    c1 = ['readelf'] + rf + [fn]
    relf = subprocess.Popen(c1, stdout=subprocess.PIPE)

    c2 = ['grep'] + gre
    grep = subprocess.Popen(c2, stdin=relf.stdout, stdout=subprocess.PIPE)
    relf.stdout.close()

    out, _ = grep.communicate()
    out = filter(None, out.split('\n'))
    return out

class Params(object):
    def __init__(self, fn):
        self._syms = {}
        self._initsym(fn, 'writeBarrier')
        self._initsym(fn, 'type\.\*')

        d = readelfgrep(fn, ['-S'], ['\.text.*PROGBIT'])[0].split()
        foff = int(d[5], 16)
        textva = int(d[4], 16)

        d = readelfgrep(fn, ['-l'], ['-E', '[[:digit:]]{2}.*\.text\>'])
        textseg = int(d[0].split()[0])

        d = readelfgrep(fn, ['-l'], ['-E', '0x[[:xdigit:]]{16}'])
        sline = d[textseg * 2 + 1]
        memsz = int(sline.split()[0], 16)

        print '.text file offset:', hex(foff)
        print '.text VA:', hex(textva), ('(seg %d)' % (textseg))

        with open('main.gobin', 'rb') as f:
            d = f.read()
        d = d[foff:]
        d = d[:memsz]
        data = d
        #data = data[:3000]

        md = Cs(CS_ARCH_X86, CS_MODE_64)
        md.detail = True
        md.syntax = CS_OPT_SYNTAX_ATT

        ilist = [x for x in md.disasm(data, textva)]
        ilist = sorted(ilist, key=lambda x: x.address)
        for i, x in enumerate(ilist):
                x.idx = i
        self._ilist = ilist

        iaddr = {}
        for x in self._ilist:
                iaddr[x.address] = x
        self._iaddr = iaddr

        self._jmps = [ X86_INS_JAE, X86_INS_JA, X86_INS_JBE, X86_INS_JB,
        X86_INS_JCXZ, X86_INS_JECXZ, X86_INS_JE, X86_INS_JGE, X86_INS_JG,
        X86_INS_JLE, X86_INS_JL, X86_INS_JMP, X86_INS_JNE, X86_INS_JNO,
        X86_INS_JNP, X86_INS_JNS, X86_INS_JO, X86_INS_JP, X86_INS_JRCXZ,
        X86_INS_JS ]

    def _initsym(self, fn, sym):
        s = symlookup(fn, sym)
        self._syms[sym] = s
        print 'SYM %s %#x %#x' % (s.name, s.start, s.end)

    def iswb(self, ins):
        if ins.id != X86_INS_MOV:
            return False
        if len(ins.operands) != 2:
            return False
        # operand indicies match chosen syntax, which is intel by default
        src, dst = ins.operands[0], ins.operands[1]
        if dst.type == X86_OP_REG and src.type == X86_OP_MEM:
            addr = src.value.mem.disp + ins.address + ins.size
            wbsym = p._syms['writeBarrier']
            if wbsym.within(addr):
                return True
        return False

    # return true if x has an operand that uses register reg
    def uses(self, x, reg):
        n = x.op_count(X86_OP_REG)
        for i in range(n):
            op = x.op_find(X86_OP_REG, i + 1)
            if op.reg == reg:
                return True
        return False

    # returns the register IDs used by instruction x
    def regops(self, ins, exp):
        ret = [x.reg for x in filter(lambda op: op.type == X86_OP_REG, ins.operands)]
        if len(ret) != exp:
            raise 'mismatch expect'
        return ret

    def next(self, x):
        return self._ilist[x.idx + 1]

    # returns the first instruction after ins which uses register reg for an
    # operand
    def findnextreg(self, ins, reg, bound):
        for i in range(bound):
            ins = self.next(ins)
            if self.uses(ins, reg):
                return ins
        raise 'didnt find within bound'

    def findnext(self, ins, xids, bound):
        for i in range(bound):
            ins = self.next(ins)
            if ins.id in xids:
                return ins
        print 'ADDR', hex(ins.address)
        raise 'didnt find within bound'

    # returns the first jump instructions after ins
    def findnextjmp(self, ins, bound):
        return self.findnext(ins, self._jmps, bound)

    # returns the first call instructions after ins
    def findnextcall(self, ins, bound):
        return self.findnext(ins, [X86_INS_CALL], bound)

    def ensure(self, ins, xids):
        if ins.id not in xids:
            print '%d != %s (%s %s)' % (ins.id, xids, ins.mnemonic, ins.op_str)
            raise 'mismatch'

    def writebarrierins(self):
        wb = []
        for x in p._ilist:
            if p.iswb(x):
                wb.append(x)
                reg = p.regops(x, 1)
                reg = reg[0]
                # find next instruction which uses the register into which the flag was
                # loaded. it should be a test.
                n = p.findnextreg(x, reg, 20)
                p.ensure(n, [X86_INS_TEST])
                wb.append(n)
                n = p.findnextjmp(n, 20)
                p.ensure(n, [X86_INS_JNE])
                wb.append(n)
                if len(n.operands) != 1:
                    raise 'no'
                # add the block executed when write barrier is enabled
                addr = n.operands[0].imm
                n = p._iaddr[addr]

                call = p.findnextcall(n, 10)
                jmp = p.findnextjmp(n, 20)
                p.ensure(jmp, [X86_INS_JMP])
                # make sure the jump comes after the call
                if jmp.address - call.address < 0:
                    raise 'call must come first'

                while n.address <= jmp.address:
                    wb.append(n)
                    n = p.next(n)
        return wb

p = Params('main.gobin')
print 'made all map: %d' % (len(p._ilist))

wb = p.writebarrierins()

with open('wb.txt', 'w') as f:
    for w in wb:
        print >> f, '%x' % (w.address)

#print 'wb list:', len(wb)
#mp = {}
#for w in wb:
#    mp[w.address] = True
#print 'wb map:', len(mp)

