#! /usr/bin/env python3

import sys

inuse = {}
blks = []

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
           blks.append(l[4])      
       print(l, extra)

print(len(blks), blks)
cnt = 0
for k, v in inuse.items():
    if v > 0:
        cnt = cnt+1
        print (k, v)
        if not k in blks:
            print("missing", k)
print("cnt", cnt)
