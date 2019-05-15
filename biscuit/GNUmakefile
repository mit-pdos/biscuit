QEMU ?= qemu-system-x86_64
PYTHON ?= python2

TOP := .

BASEFLAGS := -Wall -Werror -MMD -MP -O -g
ASFLAGS := $(BASEFLAGS) -nostdlib -nostdinc -I$(TOP)
CFLAGS := $(BASEFLAGS) -ffreestanding -nostdlib -nostdinc -fno-builtin \
	-mno-red-zone -fno-stack-protector
CXXFLAGS := $(BASEFLAGS) -ffreestanding -nostdlib -fno-builtin -mno-red-zone
GOPATH ?= $(shell pwd)
# bootloader sources
ASMS := boot.S
CS   := bootmain.c main.c chentry.c
SRCS := $(ASMS) $(CS)

# kernel sources

K := src/kernel
F := src/fs

KSRC := main.go syscall.go
KSRC := $(addprefix $(K)/,$(KSRC))
FSRC := bdev.go bitmap.go dir.go fs.go inode.go log.go super.go cache.go blk.go
FSRC := $(addprefix $(F)/,$(FSRC))
CS   := $(addprefix $(K)/,$(CS))

PSRC := src/accnt/accnt.go \
	src/ahci/ahci.go \
	src/apic/apic.go \
	src/apic/ioapic.go \
	src/hashtable/hashtable.go \
	src/bnet/net.go \
	src/bpath/bpath.go \
	src/bounds/bounds.go \
	src/caller/caller.go \
	src/defs/defs.go src/defs/errno.go src/defs/syscall.go src/defs/device.go \
	src/fd/fd.go \
	src/fdops/fdops.go \
	src/inet/inet.go \
	src/ixgbe/ixgbe.go \
	src/limits/limits.go \
	src/mem/mem.go src/mem/dmap.go \
	src/msi/msi.go \
	src/oommsg/oommsg.go \
	src/pci/pci.go src/pci/legacydisk.go src/pci/pciide.go \
	src/res/res.go \
	src/proc/proc.go src/proc/wait.go src/proc/oom.go src/proc/syscalli.go \
	src/vm/vm.go src/vm/pmap.go src/vm/as.go src/vm/rb.go src/vm/userbuf.go \
	src/stat/stat.go \
	src/stats/stats.go \
	src/tinfo/tinfo.go \
	src/ustr/ustr.go \
	src/util/util.go

OBJS := $(addprefix $(K)/, $(patsubst %.S,%.o,$(patsubst %.c,%.o,$(SRCS))))

CBINS := hello fault fork getpid fstest fswrite fsmkdir fscreat fsbigwrite \
	  fslink fsunlink fsfree ls bmwrite bmread conio lsh bmopen cls \
	  fault2 echo cat bmgc bmsmallfile bmgc2 sfork unlink touch mkdir \
	  pipetest kill killtest mmaptest usertests thtests pthtests \
	  mknodtest sockettest mv sleep time true init sync reboot ebizzy \
	  uname pwd rmtree halp less lnc rshd bimage fweb fcgi stress \
	  smallfile largefile cksum head goodcit mmapbench vary pstat

FSCPROGS := $(addprefix fsdir/bin/,$(CBINS))
CPROGS := $(addprefix user/c/,$(CBINS))

#CXXBINS := mail-enqueue mail-qman mail-deliver mailbench
FSCXXPROGS := $(addprefix fsdir/bin/,$(CXXBINS))
CXXPROGS := $(addprefix user/cxx/,$(CXXBINS))

FSPROGS := $(FSCPROGS) $(FSCXXPROGS)

BGOS := $(K)/mpentry.bin.bgo

RFS  := $(patsubst %.c,%.d,$(CS))
RFS  += $(addsuffix .d,$(CPROGS))
RFS  += $(addsuffix .d,$(CXXPROGS))
RFS  += user/c/litc.d

GOBIN := ../bin/go
SKEL := fsdir
SKELDEPS := $(shell find $(SKEL))

CPUS := $(shell echo $${CPUS:-1})

QOPTS := -m 8G -smp cpus=$(CPUS) -cpu Nehalem
# for testing bhw2's configuration
#QOPTS := -m 8G -cpu Nehalem -smp sockets=2,cores=10,threads=2

OS := $(shell uname -s)

ifeq ($(OS), OpenBSD)
	ASFLAGS += -nopie
endif

all: go.img net.img

-include $(RFS)

$(K)/boot: $(K)/boot.o $(K)/bootmain.o $(K)/stamp.py
	$(LD) --omagic -o $@.elf -m elf_i386 -static -e start -Ttext 0x7c00 \
	    $(K)/boot.o $(K)/bootmain.o
	objcopy -O binary $@.elf $@.bin
	$(PYTHON) $(K)/stamp.py $@.bin
	mv $@.bin $@

$(K)/boot.o: ASFLAGS += -m32

$(K)/bootmain.o: CFLAGS += -I$(K) -o $@
$(K)/bootmain.o: $(K)/bootmain.c
	@# don't use -Os because it rearranges code in .text such that start
	@# isn't first. htf do i force ld to put a specific function first in
	@# the section?
	@#$(CC) -c $(CFLAGS) -Os $<
	$(CC) -m32 -mno-sse -c $(CFLAGS) $<

d.img: main boot
	cat boot main > $@

mkfs: src/mkfs/mkfs.go  $(FSRC) $(PSRC) src/ufs/ufs.go
	GOPATH="$(GOPATH)" $(GOBIN) build src/mkfs/mkfs.go

go.img: $(K)/boot  $(K)/main.gobin $(SKELDEPS) $(FSPROGS) ./mkfs
	./mkfs $(K)/boot $(K)/main.gobin $@ $(SKEL) || { rm -f $@; false; }

net.img: $(K)/boot $(K)/main.gobin $(SKELDEPS) $(FSPROGS) ./mkfs
	./mkfs $(K)/boot $(K)/main.gobin $@ $(SKEL) || { rm -f $@; false; }

BHW := bhw.pdos.csail.mit.edu
net: net.img
	./net-helper.sh $^ $(BHW)

BHW2 := bhw2.pdos.csail.mit.edu
net2: net.img
	./net-helper.sh $^ $(BHW2)

$(GOBIN):
	@echo; echo; echo; \
		echo 'You need to build go:'; \
		echo 'cd ../src && ./all.bash'; \
		echo; echo; echo; \
		false

$(K)/main.gobin: chentry $(GOBIN) $(K)/bins.go $(KSRC) $(FSRC) $(PSRC)
	GOPATH="$(GOPATH)" $(GOBIN) build -o $@_ $(K)/bins.go $(KSRC)
	ADDR=0x`nm $@_ |grep _rt0_hack |cut -f1 -d' '`; \
		if test "$$ADDR" = "0x"; then echo no _rt0_hack; false; \
		else ./chentry $@_ $$ADDR; fi
	$(K)/rewrite.py $@_ $@

# the user/% prereq is built by the CPROGS target
$(FSCPROGS): fsdir/bin/% : user/c/%
	objcopy -S $^ $@

$(CPROGS): CFLAGS += -I user/c/include -fPIC -std=gnu11
$(CPROGS): % : %.c user/c/litc.o
	$(CC) $(CFLAGS) -Wl,-T user/c/linker.ld -Wl,--build-id=none \
	    -o $@ user/c/litc.o $<

$(FSCXXPROGS): fsdir/bin/% : user/cxx/%
	objcopy -S $^ $@

CXXRT := $(shell $(CC) -print-file-name=libgcc_eh.a) \
	 $(shell $(CC) -print-file-name=libsupc++.a)

CXXLOBJS := user/cxx/libutil.o user/cxx/shutil.o user/cxx/cxxrt.o \
	user/cxx/threads.o
$(CXXLOBJS): user/cxx/sysroot

CXXBEGIN := user/cxx/cxxrtbegin.o
CXXEND := user/cxx/cxxrtend.o
$(CXXPROGS): CXXFLAGS += -I user/cxx/ -I user/c/include/ \
	-isysroot user/cxx/sysroot -fPIC -DXV6_USER -std=c++11
$(CXXPROGS): % : %.cc  $(CXXLOBJS) $(CXXBEGIN) $(CXXEND) user/cxx/sysroot \
	    user/c/litc.o
	$(CXX) $(CXXFLAGS) -Wl,-T user/cxx/linker.ld -Wl,--build-id=none \
	    -o $@ $(CXXBEGIN) $< $(CXXLOBJS) user/c/litc.o $(CXXRT) $(CXXEND)

user/cxx/sysroot: user/cxx/host_hdrs.hh
	rm -rf $@.tmp $@
	mkdir -p $@.tmp
	tar c $$($(CXX) -E -H -std=c++0x -ffreestanding $< -o /dev/null 2>&1 \
		| awk '/^[.]/ {print $$2}') | tar xC $@.tmp
	mv $@.tmp $@

LINS := $(addprefix user/cxx/linux/,$(CXXBINS))
$(LINS) : user/cxx/linux/% : user/cxx/%.cc
	g++ -std=c++11 -Wall -Werror -static -o $@ $< user/cxx/libutil.cc \
		user/cxx/shutil.cc -Wl,--whole-archive -lpthread \
		-Wl,--no-whole-archive

linux: $(LINS)

$(K)/mpentry.bin: $(K)/mpentry.S
	$(CC) -static -o $(K)/mpentry.elf -Ttext 0x8000 -Wl,-n -nostdlib $^
	objcopy -O binary -j.text $(K)/mpentry.elf $@

$(BGOS): %.bgo : %
	./$(K)/bin2go.sh $< > $@

$(K)/bins.go: $(BGOS) $(K)/prepbins.py
	$(PYTHON) $(K)/prepbins.py $(BGOS) > $@_
	mv $@_ $@

main: main.o
	$(LD) -static -e main -o $@ $<

chentry: $(K)/chentry.c
	$(CC) $(BASEFLAGS) -o $@ $(K)/chentry.c

clean:
	rm -f $(BGOS) $(OBJS) $(RFS) $(K)/boot.elf $(K)/d.img $(K)/main $(K)/boot $(K)/main.gobin \
	    $(K)/go.img $(K)/chentry $(K)/mpentry.elf $(K)/mpentry.bin $(K)/_bins.go $(K)/bins.go \
	    user/c/litc.o $(FSPROGS) $(CPROGS) $(CXXPROGS) btest btest.elf \
	    $(CXXBEGIN) $(CXXEND) $(CXXLOBJS) $(LINS) $(K)/_main.gobin mkfs
	rm -rf user/cxx/sysroot

qemu: gqemu
qemux: gqemux
qemu-gdb: gqemu-gdb

QOPTS += -device ahci,id=ahci0 \
	-drive file=go.img,if=none,format=raw,id=drive-sata0-0-0 \
	-device ide-drive,drive=drive-sata0-0-0,id=sata0-0-0,bus=ahci0.0

old_qemu: d.img
	$(QEMU) $(QOPTS) -hda d.img

old_qemu-gdb: d.img
	$(QEMU) $(QOPTS) -S -s -hda d.img

gqemu: go.img
	$(QEMU) $(QOPTS) -nographic

gqemux: go.img
	$(QEMU) $(QOPTS) -serial stdio

gqemu-gdb: go.img
	$(QEMU) $(QOPTS) -nographic -S -s

gqemux-gdb: go.img
	$(QEMU) $(QOPTS) -S -s -serial stdio

btest: btest.c
	gcc -Os -m32 -I./ -o $@.elf $^ -nostdlib -nostdinc -e main -Ttext 0x7c00
	objcopy -O binary -j.text $@.elf $@

.PHONY: clean qemu qemu-gdb gqemu gqemux gqemu-gdb gqemux-gdb net linux net net2
.PRECIOUS: stamp.py
