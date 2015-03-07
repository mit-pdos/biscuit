#!/usr/bin/env python

# this script takes as input the filenames of the boot and kernel images and
# creates a disk filled with the contents of the provided skel dir directory,
# poking the block number of the first free block into the mbr of the disk
# image.

import getopt
import os
import sys

blocksz = 512
hdsize = 20 * 1024 * 1024
# number of inode direct addresses
iaddrs = 10
# number of inode indirect addresses
indaddrs = 63

class Balloc:
  def __init__(self, ff):
    self.ff = ff
    # allblocks maps each allocated block number to a block object: either a
    # Datab or an Inodeb
    self.allblocks = {}
    self.cblock = 0
  def balloc(self):
    ret = self.ff + self.cblock
    self.cblock += 1
    return ret
  def pair(self, bn, blk):
    if bn in self.allblocks:
      raise ValueError('block already paired')
    self.allblocks[bn] = blk
  def blkget(self, bn):
    return self.allblocks[bn]

class Datab:
  # holds raw block data
  def __init__(self, bn, cont=''):
    self.bn, self.cont = bn, cont
    if len(cont) > blocksz:
      raise ValueError('block too large')
  def append(self, cont):
    self.cont += cont
    if len(self.cont) > blocksz:
      raise ValueError('datablock content larger than blocksize')
  def len(self):
    return len(self.cont)

  def writeto(self, of):
    l = len(self.cont)
    of.write(self.cont)
    of.write('\0'*(blocksz - l))

class Dirb:
  # class used internally by Inodeb; it writes a directory inode to disk
  def __init__(self, bn, ba, dirpart):
    self.bn, self.ba, self.dirpart = bn, ba, dirpart
    self.size = 0
    self.blks = []
    self.curblk = None
    self.namechk = {}
    self.indirect = 0
    self.indblk = None

  def addentry(self, fn, inodeb, inodeoff):
    self.chkname(fn)
    maxfn = 14
    c = self.ensure_cont()
    if len(fn) > maxfn:
      raise ValueError('dir entry filename too long')
    l = len(fn)
    data = fn + '\0'*(maxfn - l)
    c.append(data)
    c.append(le8(biencode(inodeb, inodeoff)))

  def chkname(self, fn):
    if fn in self.namechk:
      raise ValueError('filename already exists')
    self.namechk[fn] = 1

  def ensure_cont(self):
    # returns a block with enough space for 1 directory entry, allocating a new
    # block if necessary
    direntrysz = 22
    if self.curblk is not None and blocksz - self.curblk.len() >= direntrysz:
      return self.curblk
    # allocate new directory entry block, padding old block to block size
    # (since blocksize does not divide by (dirent count * dirent size))
    if self.curblk is not None:
      direntsz = 22
      slop = (blocksz - (blocksz/direntsz)*direntsz)
      self.curblk.append('\0'*slop)

    self.size += blocksz
    # use indirect block?
    if len(self.blks) == iaddrs:
        if self.indblk == None:
            self.indblk = Indirectb(nb, self.ba)
            self.indirect = self.indblk.init()
        self.curblk = self.indblk.grow()
    else:
        nb = self.ba.balloc()
        self.curblk = Datab(nb)
        self.ba.pair(nb, self.curblk)
        self.blks.append(nb)
    return self.curblk

  def itype(self):
    return 2

class Fileb:
  # class used internally by Inodeb; it writes a file inode to disk
  def __init__(self, bn, ba):
    self.bn, self.ba = bn, ba
    self.blks = []
    self.size = 0
    self.indirect = 0
    self.indblk = None

  def itype(self):
    return 1

  def setcont(self, d):
    l = len(d)
    self.size = l
    for i in range(roundup(l, blocksz)/blocksz):
      start = i*blocksz
      end = min(start + blocksz, l)
      # use indirect block?
      if i >= iaddrs:
        # initialize indirect block
        if self.indblk == None:
          self.indblk = Indirectb(self.ba)
          self.indirect = self.indblk.init()
        db = self.indblk.grow()
        db.append(d[start:end])
        continue
      # use direct blocks
      nb = self.ba.balloc()
      db = Datab(nb, d[start:end])
      self.ba.pair(nb, db)
      self.blks.append(nb)

class Indirectb:
  # class that writes an indirect block to disk
  def __init__(self, ba):
    self.curblk = None
    self.ba = ba

  def init(self):
    # returns this indirect blocks block number
      assert self.curblk == None
      nb = self.ba.balloc()
      self.curblk = Datab(nb)
      self.ba.pair(nb, self.curblk)
      return nb

  def grow(self):
    # allocates a new block in the indirect block; returns a Datab for the new
    # block
    assert self.curblk != None
    # last 8 bytes of an indirect block reference a new indirect block
    # allocate new indirect block?
    if self.curblk.len()/8 == indaddrs:
      nb = self.ba.balloc()
      newblk = Datab(nb)
      self.ba.pair(nb, newblk)
      self.curblk.append(le8(nb))
      assert self.curblk.len() == blocksz
      self.curblk = newblk
    # finally allocate a new data block
    nb = self.ba.balloc()
    ret = Datab(nb)
    self.ba.pair(nb, ret)
    self.curblk.append(le8(nb))
    return ret

class Inodeb:
  # class for managing inodes: 4 per block
  def __init__(self, bn):
    self.bn = bn
    self.icur = 0
    self.itop = 4
    self.imap = {}

  def getfree(self):
    if self.icur >= self.itop:
      return None
    ret = self.icur
    self.icur += 1
    return ret

  def ipair(self, islot, itype):
    if islot >= self.itop:
      raise ValueError('islot too high')
    if islot in self.imap:
      raise ValueError('islot already allocated')
    self.imap[islot] = itype

  def iwrite(self, of, blk):
    def wrnum(num):
      of.write(le8(num))
    # inode type
    wrnum(blk.itype())
    # link count
    wrnum(1)
    # size in bytes
    wrnum(blk.size)
    # major
    wrnum(0)
    # minor
    wrnum(0)
    # indirect block
    wrnum(blk.indirect)
    # block addresses
    for i in blk.blks:
      wrnum(i)
    for i in range(iaddrs - len(blk.blks)):
      wrnum(0)

  def writeto(self, of):
    sortedinodes = sorted(self.imap.keys())
    for i in sortedinodes:
      blk = self.imap[i]
      self.iwrite(of, blk)
    # write unallocated inodes
    isize = (6 + iaddrs)*8
    for i in range(self.itop - len(self.imap)):
      of.write('\0'*isize)

class Fsrep:
  # class for representing the whole file system
  def __init__(self, skeldir, ba):
    if not os.path.isdir(skeldir):
      raise ValueError('%s is not a directory' % (skeldir))
    self.sd = skeldir
    self.ba = ba
    self.indf = None
    self.rootinode = None
    self.rootioff = None
    self.build()

  def build(self):
    rootinode, rioff, iblk = self.ialloc()
    rootdir = Dirb(rootinode, self.ba, '')
    iblk.ipair(rioff, rootdir)
    self.rootinode = rootinode
    self.rootioff = rioff
    self.recursedir(rootdir, self.sd)

  def ialloc(self):
    if self.indf is not None:
      indfb = self.ba.blkget(self.indf)
      rioff = indfb.getfree()
      if rioff is not None:
        return self.indf, rioff, indfb
    self.indf = self.ba.balloc()
    indfb = Inodeb(self.indf)
    self.ba.pair(self.indf, indfb)
    ioff = indfb.getfree()
    if ioff is None:
      raise RuntimeError('wut')
    return self.indf, ioff, indfb

  def recursedir(self, dirb, dirname):
    # recursively populates dir inode with the contents of the folder 'dirname'
    dirs, files = None, None
    for _, d, f in os.walk(dirname):
      # only use walk to get the current directory
      dirs = d
      files = f
      break

    # allocate file inodes, fill with data blocks
    for f in files:
      fileb, filei, inodeb = self.ialloc()
      fb = Fileb(fileb, self.ba)
      inodeb.ipair(filei, fb)
      p = os.path.join(dirname, f)
      with open(p) as injectfile:
        fb.setcont(injectfile.read())
      dirb.addentry(f, fileb, filei)

    # allocate inodes for all dirs
    rec = []
    for d in dirs:
      dib, dii, inodeb = self.ialloc()
      db = Dirb(dib, self.ba, d)
      inodeb.ipair(dii, db)
      rec.append(db)
      dirb.addentry(d, dib, dii)

    # recursively populate dirs
    for d in rec:
      newdn = os.path.join(dirname, d.dirpart)
      self.recursedir(d, newdn)

  def rooti(self):
    return self.rootinode, self.rootioff

  def writeto(self, of, remaining, dozero):
    ab = self.ba.allblocks
    if len(ab) > remaining:
      raise ValueError('skeldir too big/out of blocks')
    blockorder = sorted(ab.keys())
    for b in blockorder:
      #before = of.tell()
      ab[b].writeto(of)
      #after = of.tell()
      #diff = after - before
      #if diff % blocksz != 0:
      #  print '**** object', type(ab[b]), 'didnt write a whole block'
      #print 'wrote %d bytes' % (diff)
    # free space
    if dozero:
      for i in range(remaining - len(ab)):
        of.write('\0'*blocksz)

  def pr(self):
    print 'print all blocks'
    ab = self.ba.allblocks
    keys = sorted(ab.keys())
    for k in keys:
      if isinstance(ab[k], Inodeb):
        print '%d, Inodeb, %d inode entries' % (k, len(ab[k].imap))
        for  v in ab[k].imap.values():
          if isinstance(v, Fileb):
            print '\tfile of len %d' % (v.size)
          elif isinstance(v, Dirb):
            print '\tdirectory of len %d' % (v.size)
      elif isinstance(ab[k], Datab):
        print '%d, Datab' % (k)
      else:
        raise ValueError('unknown block')

def roundup(n, to):
  ret = n + to - 1
  return ret - (ret % to)

def fblocks(fn):
  s = os.stat(fn)
  return roundup(s.st_size, blocksz) / blocksz

def poke(data, where, num):
  data[where+0] = chr((num >> 0*8) & 0xff)
  data[where+1] = chr((num >> 1*8) & 0xff)
  data[where+2] = chr((num >> 2*8) & 0xff)
  data[where+3] = chr((num >> 3*8) & 0xff)

def le8(num):
  l = [chr((num >> i*8) & 0xff) for i in range(8)]
  return ''.join(l)

def biencode(block, ioff):
  return (block << 2) | ioff

def dofree(of, allocblocks, freeblock, freeblocklen):
  for i in range(allocblocks/8):
    of.write(chr(0xff))
  bits = allocblocks % 8
  val = 0
  for i in range(bits):
    val |= 1 << i
  of.write(chr(val))
  # pad out to a block
  bmbytes = allocblocks/8 + 1
  rem = blocksz - (bmbytes % blocksz)
  of.write('\0'*rem)
  bmblocks = roundup(bmbytes, blocksz)/blocksz
  remaining = freeblocklen - bmblocks
  if remaining < 0:
    raise ValueError('alloced too much')
  for i in range(remaining):
    of.write('\0'*blocksz)

def dofs(of, freeblock, freeblocklen, loglen, lastblock, remaining, skeldir, dozero):
  ff = freeblock + freeblocklen + loglen
  ba = Balloc(ff)

  remaining -= 1
  remaining -= freeblocklen
  remaining -= loglen
  if remaining < 0:
    raise ValueError('ruh roh')

  # start superblock: freeblock start, freeblock length, log length, and
  of.write(le8(freeblock))
  of.write(le8(freeblocklen))
  of.write(le8(loglen))

  fsrep = Fsrep(skeldir, ba)
  #fsrep.pr()

  rootinode, rootioff = fsrep.rooti()

  # write root inode to superblock
  of.write(le8(biencode(rootinode, rootioff)))
  # last block
  of.write(le8(lastblock))
  of.write('\0'*(blocksz - 5*8))

  # super block is done, write free bitmap
  dofree(of, ba.cblock, freeblock, freeblocklen)

  # write empty log blocks
  for i in range(loglen):
    of.write('\0'*blocksz)

  # finally, write fs
  fsrep.writeto(of, remaining, dozero)

if __name__ == '__main__':

  dozero = True
  opts, args = getopt.getopt(sys.argv[1:], 'n')
  for o, v in opts:
    if o == '-n':
      # don't write empty blocks
      dozero = False

  if len(args) != 4:
    print >> sys.stderr, 'usage: %s [-n] <boot image> <kernel image> <output image> <skel dir>' % (sys.argv[0])
    sys.exit(-1)

  bfn = args[0]
  kfn = args[1]
  ofn = args[2]
  skeldir = args[3]

  hdblocks = roundup(hdsize, blocksz) / blocksz
  usedblocks = fblocks(bfn)
  usedblocks += fblocks(kfn)
  remaining = hdblocks - usedblocks

  FSOFF = 506
  print >> sys.stderr, 'free blocks start at %d' % (usedblocks)
  print >> sys.stderr, 'writing fs start to %#x' % (FSOFF)

  with open(bfn, 'r') as bf, open(kfn, 'r') as kf, open(ofn, 'w') as of:
    bfdata = list(bf.read())
    kfdata = kf.read()
    poke(bfdata, FSOFF, usedblocks)

    of.write(''.join(bfdata))
    of.write(kfdata)
    # pad out kernel image to block
    lim = roundup(len(kfdata), blocksz)
    of.write('\0'*(lim - len(kfdata)))

    fblen = 10
    loglen = 31
    dofs(of, usedblocks + 1, fblen, loglen, hdblocks, remaining, skeldir, dozero)
    wrote = of.tell()/(1 << 20)

  print >> sys.stderr, 'created "%s" of length %d MB' % (ofn, wrote)
  print >> sys.stderr, '(fs starts at %#x in "%s")' % (usedblocks*blocksz, ofn)
