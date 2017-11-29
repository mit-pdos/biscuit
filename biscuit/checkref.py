import sys

inuse = {}
blks = []

start = False

with open(sys.argv[1]) as f:
   for line in f:
       l = line.split(" ")
       p = False
       if l[0] == 'usertests':
           start = True
       if not start:
           continue
       if l[0] == "bdev_refdown:" or l[0] == "?bdev_refdown:" or l[0] == "!bdev_refdown:":
           b = l[1]
           inuse[b] = inuse[b] - 1
           # if inuse[b] == 0:
           #    del inuse[b]
           p = True
       if l[0] == "bdev_refup:":
           b = l[2]
           if b in inuse:
               inuse[b] = inuse[b] + 1
           else:
               inuse[b] = 1
           p = True
       if l[0] == "block":
           blks.append(l[4])      
       print(l)

print(len(blks), blks)
cnt = 0
for k, v in inuse.items():
    if v > 0:
        cnt = cnt+1
        print (k, v)
        if not k in blks:
            print("missing", k)
print("cnt", cnt)
