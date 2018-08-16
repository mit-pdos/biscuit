#!/usr/bin/env python2
# vim: expandtab ts=4 sw=4

import piper

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

# returns a list of output lines
def readelfgrep(fn, rf, gre):
    c1 = ['readelf'] + rf + [fn]
    c2 = ['grep'] + gre
    out, _ = piper.piper([c1, c2])
    out = filter(None, out.split('\n'))
    return out

class Basicblock(object):
    def __init__(self, firstaddr, addrs, succs):
        # succs and preds are lists of the first addresses of the succesor
        # and predecessor blocks
        self.firstaddr, self.addrs, self.succs = firstaddr, addrs, succs
        self.preds = {}
        if self.firstaddr != self.addrs[0]:
            raise 'no'

class Params(object):
    def __init__(self, fn):
        self._bbsdone, self._bbret = False, None
        self._syms = {}
        self._initsym(fn, 'writeBarrier')
        self._initsym(fn, 'type.*')
        self._initsym(fn, 'panicindex')
        self._initsym(fn, 'panicslice')
        self._initsym(fn, 'panicdivide')
        #self._initsym(fn, 'panicdottype')
        self._initsym(fn, 'panicdottypeE')
        self._initsym(fn, 'panicdottypeI')

        self._stksyms = ['badmorestackg0', 'badmorestackgsignal',
        'morestackc', 'morestack', 'morestack_noctxt']

        for sym in self._stksyms:
            self._initsym(fn, sym)

        self._wbfuncs = ['gcWriteBarrier', 'wbBufFlush.func1', 'wbBufFlush', 'wbBufFlush1']
        for sym in self._wbfuncs:
            self._initsym(fn, sym)

        d = readelfgrep(fn, ['-S'], ['\.text.*PROGBIT'])[0].split()
        foff = int(d[5], 16)
        textva = int(d[4], 16)

        d = readelfgrep(fn, ['-l'], ['-E', '[[:digit:]]{2}.*\.text\>'])
        textseg = int(d[0].split()[0])

        d = readelfgrep(fn, ['-S'], ['-A1', '\.text.*PROGBIT'])[1].split()
        textsz = int(d[0], 16)
        self._endva = textva + textsz

        print '.text file offset:', hex(foff)
        print '.text endva:', hex(self._endva)
        print '.text VA:', hex(textva), ('(seg %d)' % (textseg))

        with open(fn, 'rb') as f:
            d = f.read()
        d = d[foff:]
        d = d[:textsz]
        data = d
        #data = data[:3000]

        md = Cs(CS_ARCH_X86, CS_MODE_64)
        md.detail = True
        md.syntax = CS_OPT_SYNTAX_ATT

        ilist = [x for x in md.disasm(data, textva)]
        if sum([len(x.bytes) for x in ilist]) != textsz:
            raise 'disass failed'
        ilist = sorted(ilist, key=lambda x: x.address)
        #ilist = filter(None, [x if x.address < self._endva else None for x in ilist])
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

        self._cmps = [ X86_INS_CMP, X86_INS_CMPPD, X86_INS_CMPPS,
        X86_INS_CMPSB, X86_INS_CMPSD, X86_INS_CMPSQ, X86_INS_CMPSS,
        X86_INS_CMPSW, X86_INS_TEST]

        self._condjmps = list(set(self._jmps).difference(set([X86_INS_JMP])))

    def _initsym(self, fn, sym):
        s = piper.symlookup(fn, sym)
        self._syms[sym] = s
        print 'SYM %s %#x %#x' % (s.name, s.start, s.end)

    # returns true if ins is the first instruction of a write barrier check
    def isoldwb(self, ins):
        if ins.id != X86_INS_MOV:
            return False
        if len(ins.operands) != 2:
            return False
        # operand indicies match chosen syntax, which is intel by default
        src, dst = ins.operands[0], ins.operands[1]
        if dst.type == X86_OP_REG and src.type == X86_OP_MEM:
            if src.mem.base != X86_REG_RIP:
                return False
            addr = src.value.mem.disp + ins.address + ins.size
            wbsym = self._syms['writeBarrier']
            if wbsym.within(addr):
                return True
        return False

    # returns true if ins is the first instruction of a type assertion or
    # switch
    def istc(self, ins):
        if ins.id != X86_INS_LEA:
            return False
        if len(ins.operands) != 2:
            return False
        src, dst = ins.operands[0], ins.operands[1]
        if dst.type != X86_OP_REG or src.type != X86_OP_MEM:
            return False
        if src.mem.base != X86_REG_RIP:
            return False
        addr = src.value.mem.disp + ins.address + ins.size
        typesym = self._syms['type.*']
        if not typesym.within(addr):
            return False
        reg = self.regops(ins, 1)[0]
        n = self.next(ins)
        if n.id not in [X86_INS_CMP] or not self.uses(n, reg):
            return False
        return True

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
        if x.idx + 1 >= len(self._ilist):
            return None
        return self._ilist[x.idx + 1]

    # returns the first instruction after ins which uses register reg for an
    # operand
    def findnextreg(self, ins, reg, bound):
        i = 0
        while bound == -1 or i < bound:
            i += 1
            ins = self.next(ins)
            if self.uses(ins, reg):
                return ins
        raise 'didnt find within bound'

    def findnext(self, ins, xids, bound):
        i = 0
        while bound == -1 or i < bound:
            i += 1
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
            print '%x %s (%s %s)' % (ins.address, xids, ins.mnemonic, ins.op_str)
            raise 'mismatch'

    def iswb(self, ins):
        return self._iswb1(ins) or self._iswb2(ins)

    def _iswb2(self, ins):
        if not ins.id == X86_INS_LEA:
            return False
        mem, reg = ins.operands[0], ins.operands[1]
        if mem.type != X86_OP_MEM or reg.type != X86_OP_REG:
            return False
        addr = mem.mem.disp + ins.address + ins.size
        return self._syms['writeBarrier'].within(addr)

    def _iswb1(self, ins):
        if not self.isimmcmp(ins) or ins.operands[0].imm != 0:
            return False
        op = ins.operands[1]
        if op.type != X86_OP_MEM or op.mem.base != X86_REG_RIP:
            return False
        addr = op.mem.disp + ins.address + ins.size
        return self._syms['writeBarrier'].within(addr)

    def writebarriers(self, oldstyle=False):
        '''
        finds write barrier checks of the form:
        mov     writebarrierflag, REG
        test    REG
        jnz     1
        2:
            ...
        1:
            ...
            call    writebarrierfunc
            ...
            jmpq    2

        and returns them as a list. all the instructions and "..."s above
        except for the "..." between the 2 and 1 labels are included in the
        returned set
        '''
        self.bbs()
        wb = []
        for x in self._ilist:
            found = False
            for wbfn in self._wbfuncs:
                sym = self._syms[wbfn]
                if sym.within(x.address):
                    wb.append(x)
                    found = True
                    break
            if found:
                continue
            if not oldstyle and self.iswb(x):
                # go1.10 write barriers don't load the flag to a register, but
                # compare the flag directly and the jne is always the next
                # instruction. there are also a few places in the runtime where
                # the write barrier flag is explicitly checked; those places
                # uses lea;cmp;cjmp.
                if x.id == X86_INS_CMP:
                    n = self.next(x)
                    self.ensure(n, [X86_INS_JNE])
                    wb.append(x)
                    wb.append(n)
                    baddr = n.operands[0].imm
                else:
                    n = self.next(x)
                    j = self.next(n)
                    self.ensure(x, [X86_INS_LEA])
                    if not self.isimmcmp(n) or n.operands[0].imm != 0:
                        raise 'unexpected cmp'
                    self.ensure(j, [X86_INS_JE, X86_INS_JNE])
                    wb.append(x)
                    wb.append(n)
                    wb.append(j)
                    if j.id == X86_INS_JE:
                        # writebarrier == 0, branch not taken
                        baddr = self.next(j).address
                    else:
                        baddr = j.operands[0].imm
                wblk = self.bbins(baddr)
                if len(wblk) == 0:
                    raise 'bad block'
                for ins in wblk:
                    wb.append(ins)
            elif self.isoldwb(x):
                wb.append(x)
                reg = self.regops(x, 1)
                reg = reg[0]
                # find next instruction which uses the register into which the flag was
                # loaded. it should be a test.
                n = self.findnextreg(x, reg, 20)
                self.ensure(n, [X86_INS_TEST])
                wb.append(n)
                n = self.findnextjmp(n, 20)
                self.ensure(n, [X86_INS_JNE])
                wb.append(n)
                if len(n.operands) != 1:
                    raise 'no'
                # add the block executed when write barrier is enabled
                addr = n.operands[0].imm
                n = self._iaddr[addr]

                call = self.findnextcall(n, 10)
                jmp = self.findnextjmp(n, 20)
                self.ensure(jmp, [X86_INS_JMP])
                # make sure the jump comes after the call
                if jmp.address - call.address < 0:
                    raise 'call must come first'

                while n.address <= jmp.address:
                    wb.append(n)
                    n = self.next(n)
        return [x.address for x in wb]

    def isnilchk(self, ins):
        '''
        finds all nil pointer checks of the form:
            mov     ptr, REG
            test    %al, (REG)

        at least go1.8 and go1.10.1 always uses %al for nil pointer checks
        '''
        if ins.id != X86_INS_TEST:
            return False
        if len(ins.operands) != 2:
            return False
        # operand indicies match chosen syntax, which is intel by default
        al, mem = ins.operands[0], ins.operands[1]
        if al.type != X86_OP_REG or mem.type != X86_OP_MEM:
            return False
        if al.reg != X86_REG_AL:
            return False
        if mem.mem.base == 0 or mem.mem.disp != 0 or mem.mem.index != 0 or mem.mem.scale != 1:
            return False
        return True

    def isimmcmp(self, ins):
        return ins.id == X86_INS_CMP and ins.operands[0].type == X86_OP_IMM

    def ptrchecks(self):
        ret = []
        for x in p._ilist:
            if p.isnilchk(x):
                ret.append(x.address)
        return ret

    def prbb(self, bb):
        print '------- %x -----' % (bb.firstaddr)
        print 'PREDS', ' '.join(['%x' % (x) for x in bb.preds])
        for caddr in bb.addrs:
            ins = self._iaddr[caddr]
            print '%x: %s %s' % (ins.address, ins.mnemonic, ins.op_str)
        print 'SUCS', ' '.join(['%x' % (x) for x in bb.succs])
        #print '--------------------'

    def sucsfor(self, end, bstops):
        # only a few special runtime functions (gogo, cgo, and reflection) have
        # jumps that are not immediates and can be safely ignored since they
        # are not involved in safety checks.
        sucs = []
        if end.id == X86_INS_JMP:
            # block has one successor
            if end.operands[0].type == X86_OP_IMM:
                sucs = [end.operands[0].imm]
            else:
                #print 'IND at %#x' % (end.address)
                # ignore special reg dest
                sucs = []
        elif end.id in bstops:
            # block has no successors
            sucs = []
        else:
            # must be conditional jump; block has two successors
            p.ensure(end, p._condjmps)
            sucs = [end.operands[0].imm]
            tmp = end.address + end.size
            # avoid duplicate successors if the conditional branch target
            # is also the following instruction
            if tmp in p._iaddr and tmp != sucs[0]:
                sucs.append(tmp)
        return sucs

    # returns map of first instruction of basic block to basic block
    def bbs(self):
        if self._bbsdone:
            return self._bbret
        allbs = []
        # map of all instruction addresses to basic blocks
        in2b = {}
        # the go compiler uses ud2 and int3 for padding instructions that
        # shouldn't be reached; use them as a basic block boundary too.
        bstops = [X86_INS_UD2, X86_INS_INT3, X86_INS_RET]
        bends = self._jmps + [X86_INS_RET] + bstops
        caddr = self._ilist[0].address
        while caddr in p._iaddr:
            ins = p._iaddr[caddr]
            # end == ins for single instruction blocks
            end = ins
            if end.id not in bends:
                end = self.findnext(ins, bends, -1)
            #print 'VISIT', hex(caddr), hex(ins.address)
            sucs = self.sucsfor(end, bstops)
            baddrs = []
            while ins is not None and ins.address <= end.address:
                baddrs.append(ins.address)
                ins = p.next(ins)
            newb = Basicblock(baddrs[0], baddrs, sucs)
            for addr in baddrs:
                in2b[addr] = newb
            allbs.append(newb)

            caddr = end.address + end.size

        allbs = sorted(allbs, key=lambda x:x.firstaddr)
        for b in allbs:
            if len(b.addrs) == 0:
                raise 'no'
            #p.prbb(b)

        # pass two creates predecessor lists
        for b in allbs:
            for s in b.succs:
                tb = in2b[s]
                tb.preds[b.firstaddr] = True
        # sanity
        for b in allbs:
            if len(b.succs) > 2:
                raise 'no'
            if len(b.succs) == 2 and b.succs[0] == b.succs[1]:
                p.prbb(b)
                raise 'no'
        self._bb = in2b
        self._bbret = [x.firstaddr for x in allbs]
        self._bbsdone = True
        return self._bbret

    # returns list of instructions for the basic block containing baddr
    def bbins(self, baddr):
        return [self._iaddr[x] for x in p._bb[baddr].addrs]

    def iscndjmp(self, ins):
        return ins in self._condjmps

    def isboundpanic(self, baddr):
        for ins in self.bbins(baddr):
            if ins.id == X86_INS_CALL and ins.operands[0].type == X86_OP_IMM:
                for sn in ['panicindex', 'panicslice']:
                    sym = p._syms[sn]
                    if sym.within(ins.operands[0].imm):
                        return True
        return False

    def isdividepanic(self, baddr):
        for ins in self.bbins(baddr):
            if ins.id == X86_INS_CALL and ins.operands[0].type == X86_OP_IMM:
                if self._syms['panicdivide'].within(ins.operands[0].imm):
                    return True
        return False

    def istypepanic(self, baddr):
        for ins in self.bbins(baddr):
            if ins.id == X86_INS_CALL and ins.operands[0].type == X86_OP_IMM:
                #if self._syms['panicdottype'].within(ins.operands[0].imm):
                #    return True
                panics = ['panicdottypeE', 'panicdottypeI']
                #panics = ['panicdottype']
                for p in panics:
                    sym = self._syms[p]
                    if sym.within(ins.operands[0].imm):
                        return True
        return False

    # returns the list of all conditional jumps that may reach baddr
    def _pcjmp(self, baddr):
        bb = self._bb[baddr]
        if len(bb.preds) == 0:
            raise 'no cjmp'
        ret = []
        for pa in bb.preds:
            ins = self.bbins(pa)
            if ins[-1].id in self._condjmps:
                ret.append(ins[-1].address)
            else:
                ret += self._pcjmp(pa)
        return ret

    def prevcjmps(self, baddr):
        cjaddr = self._pcjmp(baddr)
        return cjaddr

    # returns list of addresses for all compares immediately prior to address
    # jaddr
    def _prev(self, jaddr, whichf, visited, mustexist, dyump=False):
        bb = self._bb[jaddr]
        if dyump:
            #print 'VISIT', hex(jaddr)
            self.prbb(bb)
        if bb.firstaddr in visited:
            return [], []
        visited[bb.firstaddr] = True
        ins = self.bbins(jaddr)
        if jaddr == bb.firstaddr:
            jaddr = ins[-1].address
        ret = []
        for i in range(len(ins) - 1, -1, -1):
            if ins[i].address >= jaddr:
                continue
            if whichf(ins[i]):
                ret.append(ins[i].address)
                break
        morejumps = []
        preds = bb.preds
        if len(ret) == 0:
            # no cmp yet found, keep looking in predecessors
            if len(bb.preds) == 0:
                if mustexist:
                    raise 'never found'
                return [], []
        else:
            if ins[-1].id in self._condjmps:
                morejumps = [ins[-1].address]
            # we found a compare, but a predecessor's compare may be
            # immediately prior to jaddr if it is followed by a jump after the
            # found compare (to an address other than the start address)
            preds = []
            for pa in bb.preds:
                pbb = self._bb[pa]
                for sa in pbb.succs:
                    if self._bb[sa] == bb and sa > ret[0]:
                        # jump inside the block
                        preds.append(pa)
        for pa in preds:
            a, b = self._prev(pa, whichf, visited, mustexist, dyump)
            ret += a
            morejumps += b
        return ret, morejumps

    def prevcmps(self, baddr):
        return self._prev(baddr, lambda x: x.id in self._cmps, {}, True)

    def _regboth(self, ins, reg):
        d = {
            X86_REG_EAX: X86_REG_RAX,
            X86_REG_EBP: X86_REG_RBP,
            X86_REG_EBX: X86_REG_RBX,
            X86_REG_ECX: X86_REG_RCX,
            X86_REG_EDI: X86_REG_RDI,
            X86_REG_EDX: X86_REG_RDX,
            X86_REG_ESI: X86_REG_RSI,
            X86_REG_ESP: X86_REG_RSP,
            X86_REG_RAX: X86_REG_EAX,
            X86_REG_RBP: X86_REG_EBP,
            X86_REG_RBX: X86_REG_EBX,
            X86_REG_RCX: X86_REG_ECX,
            X86_REG_RDI: X86_REG_EDI,
            X86_REG_RDX: X86_REG_EDX,
            X86_REG_RIP: X86_REG_EIP,
            X86_REG_RSI: X86_REG_ESI,
            X86_REG_RSP: X86_REG_ESP,
            X86_REG_R8: X86_REG_R8D,
            X86_REG_R9: X86_REG_R9D,
            X86_REG_R10: X86_REG_R10D,
            X86_REG_R11: X86_REG_R11D,
            X86_REG_R12: X86_REG_R12D,
            X86_REG_R13: X86_REG_R13D,
            X86_REG_R14: X86_REG_R14D,
            X86_REG_R15: X86_REG_R15D,
            X86_REG_R8D: X86_REG_R8,
            X86_REG_R9D: X86_REG_R9,
            X86_REG_R10D: X86_REG_R10,
            X86_REG_R11D: X86_REG_R11,
            X86_REG_R12D: X86_REG_R12,
            X86_REG_R13D: X86_REG_R13,
            X86_REG_R14D: X86_REG_R14,
            X86_REG_R15D: X86_REG_R15,
            }

        if reg not in d:
            #print 'NO', ins.reg_name(reg)
            return [reg]
        return [reg, d[reg]]

    def boundschecks(self):
        bbs = self.bbs()
        binst = []
        for baddr in bbs:
            #self.prbb(baddr)
            if not self.isboundpanic(baddr):
                continue
            binst += self._bb[baddr].addrs

            cjmps = self.prevcjmps(baddr)
            for cj in cjmps:
                self.ensure(self._iaddr[cj], self._condjmps)
                binst.append(cj)
            cmps = []
            for cj in cjmps:
                a, b = self.prevcmps(cj)
                cmps += a
                binst += b
            for cm in cmps:
                ins = self._iaddr[cm]
                self.ensure(ins, [X86_INS_CMP, X86_INS_TEST])
                binst.append(cm)
                if ins.operands[0].type != X86_OP_REG:
                    continue
                boundregs = self._regboth(ins, ins.operands[0].reg)
                def which(ti):
                    ok = [X86_INS_MOV, X86_INS_MOVZX, X86_INS_MOVABS,
                    X86_INS_LEA, X86_INS_XOR]
                    if ti.id not in ok or ti.operands[1].type != X86_OP_REG:
                        return False
                    return ti.operands[1].reg in boundregs
                loads, _ = self._prev(cm, which, {}, True)
                #if len(loads) == 0:
                #    print 'FAILED START', hex(cm), boundregs
                #    loads = self._prev(cm, which, {}, False, True)
                #    print 'DONE'
                binst += loads
        uniq = {}
        for b in binst:
            uniq[b] = True
        return uniq.keys()

    def dividechecks(self):
        bbs = self.bbs()
        binst = []
        for baddr in bbs:
            if not self.isdividepanic(baddr):
                continue
            binst += self._bb[baddr].addrs

            cjmps = self.prevcjmps(baddr)
            for cj in cjmps:
                self.ensure(self._iaddr[cj], self._condjmps)
                binst.append(cj)
            cmps = []
            for cj in cjmps:
                a, b = self.prevcmps(cj)
                cmps += a
                binst += cmps
                binst += b
                for c in cmps:
                    self.ensure(self._iaddr[c], [X86_INS_TEST, X86_INS_CMP])
        uniq = {}
        for b in binst:
            uniq[b] = True
        return uniq.keys()

    # this only finds type assertion checks, not type switches
    def typechecks(self):
        bbs = self.bbs()
        binst = []
        for baddr in bbs:
            if not self.istypepanic(baddr):
                continue
            binst += self._bb[baddr].addrs

            cjmps = self.prevcjmps(baddr)
            for cj in cjmps:
                self.ensure(self._iaddr[cj], self._condjmps)
                binst.append(cj)
            cmps = []
            for cj in cjmps:
                a, b = self.prevcmps(cj)
                cmps += a
                binst += cmps
                binst += b
        # no need to backtrack to find the load of the type since all such
        # loads load from a known symbol that we can easily find. go1.8
        # compiler always uses lea with %rip. this will return more
        # instructions than are actually used by the type checks (for instance,
        # the leas in type information for map operations, but probably not
        # enough to matter.
        for ins in self._ilist:
            if ins.id not in [X86_INS_LEA]:
                continue
            op = ins.operands[0]
            if op.type != X86_OP_MEM or op.mem.base != X86_REG_RIP:
                continue
            addr = op.mem.disp + ins.address + ins.size
            if self._syms['type.*'].within(addr):
                binst.append(ins.address)
        uniq = {}
        for b in binst:
            uniq[b] = True
        return uniq.keys()

    def _withinstksyms(self, addr):
        for sn in self._stksyms:
            sym = self._syms[sn]
            if sym.within(addr):
                return True
        return False

    def issplitcall(self, baddr):
        for ins in self.bbins(baddr):
            if ins.id == X86_INS_CALL and ins.operands[0].type == X86_OP_IMM:
                return self._withinstksyms(ins.operands[0].imm)
        return False

    def splits(self):
        bbs = self.bbs()
        binst = []
        for baddr in bbs:
            if not self.issplitcall(baddr):
                continue
            binst += self._bb[baddr].addrs
            # the stack checks are the immediate predeccesor of the stack split
            # call
            bb = self._bb[baddr]
            for pred in bb.preds:
                pins = self.bbins(pred)
                # paranoia
                c = self.findnext(pins[0], [X86_INS_CMP], len(pins))
                binst += [x.address for x in pins]
        return binst

def writerips(rips, fn):
    print 'writing "%s"...' % (fn)
    with open(fn, 'w') as f:
        for w in rips:
            print >> f, '%x' % (w)

# writefake creates a fake profile (for munch.py) using the instructions in
# rips; use "munch.py -d" to manually inspect the instructions identified as
# HLL tax instructions in the kernel binary.
def writefake(rips, fn):
    print 'writing fake prof "%s"...' % (fn)
    with open(fn, 'w') as f:
        for w in rips:
            print >> f, '%x -- 1' % (w)

#p = Params('main.gobin')
p = Params('kernel.gobin')
print 'made all map: %d' % (len(p._ilist))

wb = p.writebarriers()
print 'found', len(wb)
writerips(wb, 'wbars.rips')

ptr = p.ptrchecks()
writerips(ptr, 'nilptrs.rips')
writefake(ptr, 'fake-nilptrs.txt')

bc = p.boundschecks()
writerips(bc, 'bounds.rips')
writefake(bc, 'fake-bounds.txt')

div = p.dividechecks()
writerips(div, 'div.rips')
writefake(div, 'fake-div.txt')

tp = p.typechecks()
writerips(tp, 'types.rips')
writefake(tp, 'fake-types.txt')

ss = p.splits()
writerips(ss, 'splits.rips')

#for bi in found:
#    print '%x' % (bi)
