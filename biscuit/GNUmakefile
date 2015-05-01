TOP := .

BASEFLAGS := -Wall -Werror -MMD -MP -O -g
ASFLAGS := $(BASEFLAGS) -nostdlib -nostdinc -I$(TOP) -m32
CFLAGS := $(BASEFLAGS) -nostdlib -nostdinc -I$(TOP) -fno-stack-protector -fno-builtin

# bootloader sources
ASMS := boot.S
CS   := bootmain.c main.c chentry.c
SRCS := $(ASMS) $(CS)

# kernel sources
KSRC := main.go syscall.go pmap.go fs.go hw.go

OBJS := $(patsubst %.S,%.o,$(patsubst %.c,%.o,$(SRCS)))

UBINS := hello fault fork getpid fstest fswrite fsmkdir fscreat fsbigwrite \
	  fslink fsunlink fsfree ls bmwrite bmread conio lsh bmopen cls \
	  fault2 echo cat bmgc bmsmallfile bmgc2 sfork unlink touch mkdir \
	  pipetest kill killtest
FSUPROGS := $(patsubst %,fsdir/bin/%,$(UBINS))
UPROGS := $(patsubst %,user/%,$(UBINS))

BGOS := mpentry.bin.bgo

RFS  := $(patsubst %.c,%.d,$(CS))
RFS  += $(patsubst %,%.d,$(UPROGS))
RFS  += user/litc.d

GOBIN := ../bin/go
SKEL := fsdir
SKELDEPS := $(shell find $(SKEL))

CPUS := $(shell echo $${CPUS:-1})
QOPTS := -m 256M -smp cpus=$(CPUS)

OS := $(shell uname -s)

QEMU := qemu-system-x86_64

ifeq ($(OS), OpenBSD)
	ASFLAGS += -nopie
endif

all: go.img net.img

-include $(RFS)

boot: boot.o bootmain.o
	$(LD) --omagic -o $@.elf -m elf_i386 -static -e start -Ttext 0x7c00 $^
	objcopy -O binary $@.elf $@.bin
	./stamp.py $@.bin
	mv $@.bin $@

bootmain.o: bootmain.c
	@# don't use -Os because it rearranges code in .text such that start
	@# isn't first. htf do i force ld to put a specific function first in
	@# the section?
	@#$(CC) -c $(CFLAGS) -Os $<
	$(CC) -m32 -c $(CFLAGS) $<

d.img: main boot
	cat boot main > $@

go.img: boot main.gobin $(SKELDEPS) $(FSUPROGS)
	./mkbdisk.py boot main.gobin $@ $(SKEL) || { rm -f $@; false; }

net.img: boot main.gobin $(SKELDEPS) $(FSUPROGS)
	./mkbdisk.py -n boot main.gobin $@ $(SKEL) || { rm -f $@; false; }

net: net.img
	@echo; echo waiting for client copy...
	@sha1sum net.img
	nc -l -p 31337 -q2 < $^
	@sha1sum net.img
	@echo; echo copied Biscuit image; echo

# the user/% prereq is built by the UPROGS target
$(FSUPROGS): fsdir/bin/% : user/%
	objcopy -S $^ $@

$(GOBIN):
	@echo; echo; echo; \
		echo 'You need to build go:'; \
		echo 'cd ../src && ./all.bash'; \
		echo; echo; echo; \
		false

main.gobin: chentry $(GOBIN) bins.go $(KSRC)
	$(GOBIN) build -o $@ bins.go $(KSRC)
	ADDR=0x`nm main.gobin |grep _rt0_hack |cut -f1 -d' '`; \
		if test "$$ADDR" = "0x"; then echo no _rt0_hack; false; \
		else ./chentry $@ $$ADDR; fi \

$(UPROGS): CFLAGS += -I user/ -fPIC
$(UPROGS): % : %.c user/litc.o
	$(CC) $(CFLAGS) -I user/ -Wl,-T user/linker.ld \
	    -Wl,-n -o $@ $< user/litc.o

mpentry.bin: mpentry.S
	$(CC) -o mpentry.elf -Ttext 0x8000 -Wl,-n -nostdlib $^
	objcopy -O binary -j.text mpentry.elf $@

$(BGOS): %.bgo : %
	./bin2go.sh $< > $@

bins.go: $(BGOS) prepbins.py
	./prepbins.py $(BGOS) > $@

main: main.o
	$(LD) -static -e main -o $@ $<

chentry: chentry.c
	$(CC) $(BASEFLAGS) -o $@ chentry.c

clean:
	rm -f $(BGOS) $(OBJS) $(RFS) boot.elf d.img main boot main.gobin \
	    go.img chentry mpentry.elf mpentry.bin bins.go user/litc.o \
	    $(FSUPROGS) $(UPROGS) btest btest.elf

qemu: d.img
	$(QEMU) $(QOPTS) -hda d.img

qemu-gdb: d.img
	$(QEMU) $(QOPTS) -S -s -hda d.img

gqemu: go.img
	$(QEMU) $(QOPTS) -nographic -hda go.img

gqemux: go.img
	$(QEMU) $(QOPTS) -serial stdio -hda go.img

gqemu-gdb: go.img
	$(QEMU) $(QOPTS) -nographic -S -s -hda go.img

gqemux-gdb: go.img
	$(QEMU) $(QOPTS) -S -s -serial stdio -hda go.img

btest: btest.c
	gcc -Os -m32 -I./ -o $@.elf $^ -nostdlib -nostdinc -e main -Ttext 0x7c00
	objcopy -O binary -j.text $@.elf $@

.PHONY: clean qemu qemu-gdb gqemu gqemux gqemu-gdb gqemux-gdb net
