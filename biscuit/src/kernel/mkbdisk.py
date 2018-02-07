#!/usr/bin/env python

# this script takes as input the filenames of the boot and kernel images and
# creates a disk filled with the contents of the provided skel dir directory,
# poking the block number of the first free block into the mbr of the disk
# image.

import getopt
import os
import sys

blocksz = 4096         # block size
inodesz = 128          # size of inodes in bytes
hdsize = 50000         # size of disk in blocks
loglen = 256           # number of log blocks
inodelen = 50          # number of inode map blocks
loginodeblk = 5        # log of number of inodes per block
iaddrs = 9             # number of inode direct addresses
indaddrs = (blocksz/8) # number of inode indirect addresses


class Balloc:
  def __init__(self, ff):
    self.ff = ff
    # allblocks maps each allocated block number to a Datab
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


class Ialloc:
  def __init__(self):
    # allblocks maps each allocated inode number to an Inode object
    self.allinodes = {}
    self.cinode = 0
    
  def ialloc(self):
    ret = self.cinode
    self.cinode += 1
    return ret
  
  def pair(self, inum, inode):
    if inum in self.allinodes:
      raise ValueError('block already paired')
    self.allinodes[inum] = inode
    
  def iget(self, inum):
    return self.allinodes[inum]

class Inode:
  def __init__(self, type, ia):
    self.inum = ia.ialloc()
    self.blks = []
    self.size = 0
    self.type = type
    self.indbn = 0
    self.indblk = None
    self.dindbn = 0
    self.dindblk = None

  def __str__(self):
    return "inum %d type %d sz %d" % (self.inum, self.type, self.size) + " " + str(self.blks)
  
  def writeto(self, of):
    def wrnum(num):
      of.write(le8(num))
    # inode type
    wrnum(self.type)
    # link count
    wrnum(1)
    # size in bytes
    wrnum(self.size)
    # major
    wrnum(0)
    # minor
    wrnum(0)
    # indirect block
    wrnum(self.indbn)
    # dindirect block
    wrnum(self.dindbn)
    # block addresses
    for i in self.blks:
      wrnum(i)
    for i in range(iaddrs - len(self.blks)):
      wrnum(0)

  # write unallocated inodes
  def iwritez(self, of):
    isize = (7 + iaddrs)*8
    of.write('\0'*isize)

  def pr(self):
    print self.inum, '\t len %d' % self.size, self.blks


class Dir(Inode):

  def __init__(self, pinum, ba, ia, dirpart):
    Inode.__init__(self, 2, ia)
    self.ba = ba
    self.dirpart = dirpart
    self.curblk = None
    self.namechk = {}
    self.addentry('.', self.inum)
    self.addentry('..', pinum)
    ia.pair(self.inum, self)

  def addentry(self, fn, inum):
    self.chkname(fn)
    maxfn = 14
    c = self.ensure_cont()
    if len(fn) > maxfn:
      raise ValueError('dir entry filename too long')
    l = len(fn)
    data = fn + '\0'*(maxfn - l)
    c.append(data)
    c.append(le8(inum))

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
            self.indblk = Indirectb(self.ba)
            self.indbn = self.indblk.init()
        self.curblk = self.indblk.grow()
    else:
        nb = self.ba.balloc()
        self.curblk = Datab(nb)
        self.ba.pair(nb, self.curblk)
        self.blks.append(nb)
    return self.curblk

    # while len(cont) > 0:
    #   e = cont[0:22]
    #   cont = cont[22:]
    #   n = e[14:18]
    #   m = e[18:22]
    #   print e, len(e), n, m
  
class File(Inode):

  def __init__(self, ba, ia):
    Inode.__init__(self, 1, ia)
    self.ba = ba
    self.curindblk = None
    ia.pair(self.inum, self)

  def setcont(self, d):
    l = len(d)
    self.size = l
    for i in range(roundup(l, blocksz)/blocksz):
      start = i*blocksz
      end = min(start + blocksz, l)
      nb = i
      # use direct blocks
      if nb < iaddrs:
        nb = self.ba.balloc()
        db = Datab(nb, d[start:end])
        self.ba.pair(nb, db)
        self.blks.append(nb)
        continue
      nb -= iaddrs

      # use indirect block?
      if nb < indaddrs:
        # initialize indirect block
        if self.indblk == None:
          self.indblk = Indirectb(self.ba)
          self.indbn = self.indblk.init()
        db = self.indblk.grow()
        db.append(d[start:end])
        continue

      # use double inblk?
      if bn < indaddrs * indaddrs:
        bn -= indaddrs

        # allocate double?
        if self.dindblk == None:
          print "allocate double", bn
          self.dindblk = Indirectb(self.ba)
          self.dindbn = self.dindblk.init()

        bno = bn % indaddrs
        if bno == 0:  # allocate indirect block?
          print "allocate indirect for double", bn
          self.curindblk = self.dindblk.grow(true)

        db = self.curindblk.grow()
        db.append(d[start:end])
        continue

      raise ValueError('file is too large')
          
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

  def grow(self, indirect=False):
    # allocates a new block in the indirect block; returns a Datab for the new
    # block
    assert self.curblk != None
    # allocate a new data/indirect block
    nb = self.ba.balloc()
    if indirect:
      ret = Indirectb(nb)
      ret.init()
    else:
      ret = Datab(nb)
    self.ba.pair(nb, ret)
    self.curblk.append(le8(nb))
    assert self.curblk.len() <= blocksz
    return ret

class Fsrep:
  # class for representing the whole file system
  def __init__(self, skeldir, ba, ia):
    if not os.path.isdir(skeldir):
      raise ValueError('%s is not a directory' % (skeldir))
    self.sd = skeldir
    self.ba = ba
    self.ia = ia
    self.indf = None
    self.build()

  def build(self):
    rootdir = Dir(0, self.ba, self.ia, '')
    self.recursedir(rootdir, self.sd)

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
      fb = File(self.ba, self.ia)
      p = os.path.join(dirname, f)
      with open(p) as injectfile:
        fb.setcont(injectfile.read())
      dirb.addentry(f, fb.inum)

    # allocate inodes for all dirs
    rec = []
    for d in dirs:
      db = Dir(dirb.inum, self.ba, self.ia, d)
      rec.append(db)
      dirb.addentry(d, db.inum)

    # recursively populate dirs
    for d in rec:
      newdn = os.path.join(dirname, d.dirpart)
      self.recursedir(d, newdn)

  def writeto(self, of, remaining, dozero):
    ai = self.ia.allinodes
    assert len(ai) <= remaining
    order = sorted(ai.keys())
    for i in order:
      # print of.tell()/blocksz, ai[i]
      before = of.tell()
      ai[i].writeto(of)
      after = of.tell()
      diff = after - before
      assert diff % inodesz == 0

    # free inodes
    ninode = (inodelen*blocksz)/inodesz - len(ai)
    assert ninode/blocksz <= remaining
    for i in range(ninode):
      # fill with crap to detect fs bugs
      of.write('\x0d'*inodesz)

    # blocks
    ab = self.ba.allblocks
    assert len(ab) <= remaining
    blockorder = sorted(ab.keys())
    for b in blockorder:
      assert of.tell()/blocksz == b
      before = of.tell()
      ab[b].writeto(of)
      after = of.tell()
      diff = after - before
      assert diff % blocksz == 0

    # free space
    if dozero:
      for i in range(remaining - len(ab)):
	# fill with crap to detect fs bugs
        of.write('\x0c'*blocksz)
      assert hdsize == of.tell()/blocksz

def roundup(n, to):
  ret = n + to - 1
  return ret - (ret % to)

def fblocks(fn):
  s = os.stat(fn)
  return s.st_size

def poke(data, where, num):
  data[where+0] = chr((num >> 0*8) & 0xff)
  data[where+1] = chr((num >> 1*8) & 0xff)
  data[where+2] = chr((num >> 2*8) & 0xff)
  data[where+3] = chr((num >> 3*8) & 0xff)

def le8(num):
  l = [chr((num >> i*8) & 0xff) for i in range(8)]
  return ''.join(l)

def dofreemap(of, nallocated, freeblock, freeblocklen):
  assert freeblock == of.tell()/blocksz

  for i in range(nallocated/8):
    of.write(chr(0xff))
    
  bits = nallocated % 8
  val = 0
  for i in range(bits):
    val |= 1 << i
  bmbytes = nallocated/8
  # nallocated divides by 8?
  if val != 0:
    of.write(chr(val))
    bmbytes += 1
  # pad out to a block
  rem = roundup(bmbytes, blocksz) - bmbytes
  of.write('\0'*rem)

  bmblocks = roundup(bmbytes, blocksz)/blocksz
  remaining = freeblocklen - bmblocks
  assert remaining >= 0
  for i in range(remaining):
    of.write('\0'*blocksz)
    
  assert freeblock+freeblocklen == of.tell()/blocksz
    

def dofs(of, used, skeldir, dozero):
  superblock = used
  assert superblock == of.tell()/blocksz
  used += 1  # for super block
  used += loglen

  imapstart = used
  imapsz = (inodelen*inodesz) / blocksz + 1
  used += imapsz

  bmapstart = used
  bmapsz = hdsize - used - inodelen
  bmapsz = bmapsz / blocksz + 1
  used += bmapsz

  used += inodelen

  ba = Balloc(used)
  bi = Ialloc()

  remaining = hdsize - used 
  if remaining < 0:
    raise ValueError('hard disk too small')

  # superblock
  print "super:", "blkn", superblock
  print "\tloglen", loglen
  print "\timapstart", imapstart, "imapsz", imapsz
  print "\tbmapstart", bmapstart, "bmapsz", bmapsz
  print "\tinodelen", inodelen
  print "\thdsize", hdsize

  of.write(le8(loglen))
  of.write(le8(imapstart))
  of.write(le8(imapsz))
  of.write(le8(bmapstart))
  of.write(le8(bmapsz))
  of.write(le8(inodelen))
  of.write(le8(hdsize))  
  of.write('\0'*(blocksz - 7*8))
  
  fsrep = Fsrep(skeldir, ba, bi)

  # write empty log blocks
  for i in range(loglen):
    of.write('\0'*blocksz)

  assert imapstart == of.tell()/blocksz
  # write free inode bitmap
  dofreemap(of, bi.cinode, imapstart, imapsz)
  assert imapstart+imapsz == of.tell()/blocksz

  assert bmapstart == of.tell()/blocksz
  dofreemap(of, ba.cblock, bmapstart, bmapsz)
  assert bmapstart+bmapsz == of.tell()/blocksz

  # finally, write fs
  fsrep.writeto(of, remaining, dozero)


if __name__ == '__main__':

  opts, args = getopt.getopt(sys.argv[1:], 'n')
  dozero = True
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

  usedblocks = fblocks(bfn)
  usedblocks += fblocks(kfn)
  usedblocks = roundup(usedblocks, blocksz) / blocksz
  
  FSOFF = 506
  print >> sys.stderr, 'free blocks start at %d' % (usedblocks)
  print >> sys.stderr, 'writing fs start to %#x' % (FSOFF)

  with open(bfn, 'r') as bf, open(kfn, 'r') as kf, open(ofn, 'w') as of:
    bfdata = list(bf.read())
    kfdata = kf.read()
    poke(bfdata, FSOFF, usedblocks)

    of.write(''.join(bfdata))

    of.write(kfdata)

    lim = roundup(len(kfdata)+len(bfdata), blocksz)
    of.write('\0'*(lim - len(kfdata)-len(bfdata)))

    if of.tell()%blocksz != 0:
      print "*** fs doesn't start at block boundary!! ****"

    if of.tell()/blocksz != usedblocks:
      print "*** used blocks don't match", of.tell() / blocksz, usedblocks

    dofs(of, usedblocks, skeldir, dozero)

    wrote = of.tell()/(1 << 20)

  print >> sys.stderr, 'created "%s" of length %d MB' % (ofn, wrote)
  print >> sys.stderr, '(fs starts at %#x(%d) in "%s")' % (usedblocks*blocksz, usedblocks, ofn)
