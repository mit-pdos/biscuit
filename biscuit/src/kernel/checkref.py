#! /usr/bin/env python3

import sys

inuse = {}
refcnt = {}
pgis = {}

start = False

with open(sys.argv[1]) as f:
   for line in f:
       l = line.split(" ")
       p = False
       extra = ""
       if l[0] == 'usertests':
           start = True
       if not start:
           continue
       if l[0] == "bdev_refdown:" or l[0] == "?bdev_refdown:" or l[0] == "!bdev_refdown:":
           b = l[2]
           inuse[b] = inuse[b] - 1
           # if inuse[b] == 0:
           #    del inuse[b]
           extra = inuse[b]
           p = True
       if l[0] == "bdev_refup:" or l[0] == "?bdev_refup:" or l[0] == "!bdev_refup:":
           b = l[3]
           if b in inuse:
               inuse[b] = inuse[b] + 1
           else:
               inuse[b] = 1
           extra = inuse[b]
           p = True
       if l[0] == "block":
           refcnt[l[4]] = l[8].rstrip('\n')
       if l[0] == "pgi":
           pgis[l[1].rstrip('\n')] = ""
       print(l, extra)

cnt = 0
ninuse = 0
for k, v in inuse.items():
    if v > 0:
        cnt = cnt+1
        if v > 1:
            ninuse += 1
        print (k, v)
        if not k in refcnt.keys():
            print("missing", k)
        else:
            if int(refcnt[k]) != int(v):
                print("refcnt doesn't match", k, v, refcnt[k])
        if v > 5:
            print("high cnt", k, v)

print("cnt in-use", cnt, ninuse)
