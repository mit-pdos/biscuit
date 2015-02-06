TOP := .

BASEFLAGS := -Wall -Werror -MMD -MP -O -g
ASFLAGS := $(BASEFLAGS) -nostdlib -nostdinc -I$(TOP) -m32
CFLAGS := $(BASEFLAGS) -nostdlib -nostdinc -I$(TOP) -fno-stack-protector -fno-builtin

# bootloader sources
ASMS := boot.S
CS   := bootmain.c main.c chentry.c
SRCS := $(ASMS) $(CS)

# kernel sources
KSRC := main.go syscall.go pmap.go

OBJS := $(patsubst %.S,%.o,$(patsubst %.c,%.o,$(SRCS)))

UPROGS := hello fault
UPROGS := $(patsubst %,user/%,$(UPROGS))

BGOS := $(patsubst %,%.bgo,$(UPROGS)) mpentry.bin.bgo

RFS  := $(patsubst %.c,%.d,$(CS))
RFS  += $(patsubst %,%.d,$(UPROGS))
RFS  += user/litc.d

GOBIN := ../bin/go

CPUS := $(shell echo $${CPUS:-1})
QOPTS := -m 256M -smp cpus=$(CPUS)

OS := $(shell uname -s)

ifeq ($(OS), OpenBSD)
	ASFLAGS += -nopie
endif

all: d.img go.img

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

go.img: boot main.gobin
	cat boot main.gobin > $@

$(GOBIN):
	@echo; echo; echo; \
		echo 'You need to build go:'; \
		echo 'cd ../src && ./all.bash'; \
		echo; echo; echo; \
		false

main.gobin: chentry $(GOBIN) $(UPROGS) bins.go $(KSRC)
	$(GOBIN) build -o $@ bins.go $(KSRC)
	ADDR=0x`nm main.gobin |grep _rt0_hack |cut -f1 -d' '`; \
		if test "$$ADDR" = "0x"; then echo no _rt0_hack; false; \
		else ./chentry $@ $$ADDR; fi \

$(UPROGS): CFLAGS += -I user/ -Wl,-entry,_entry -fPIC
$(UPROGS): % : %.c user/litc.o
	$(CC) $(CFLAGS) -I user/ -Wl,-Ttext,0xf1000000\
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
	    go.img chentry mpentry.elf mpentry.bin bins.go user/litc.o

qemu: d.img
	qemu-system-x86_64 $(QOPTS) -hda d.img

qemu-gdb: d.img
	qemu-system-x86_64 $(QOPTS) -S -s -hda d.img

gqemu: go.img
	qemu-system-x86_64 $(QOPTS) -nographic -hda go.img

gqemux: go.img
	qemu-system-x86_64 $(QOPTS) -hda go.img

gqemu-gdb: go.img
	qemu-system-x86_64 $(QOPTS) -nographic -S -s -hda go.img

gqemu-gdbx: go.img
	qemu-system-x86_64 $(QOPTS) -nographic -S -s -hda go.img

.PHONY: clean qemu gqemu
