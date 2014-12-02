TOP := .

BASEFLAGS := -Wall -Werror -MMD -MP
ASFLAGS := $(BASEFLAGS) -nostdinc -I$(TOP) -m32
CFLAGS := $(ASFLAGS) -fno-stack-protector -fno-builtin

ASMS := boot.S
CS   := bootmain.c main.c chentry.c
SRCS := $(ASMS) $(CS)
OBJS := $(patsubst %.S,%.o,$(patsubst %.c,%.o,$(SRCS)))

RFS  := $(patsubst %.c,%.d,$(CS))

GOBIN := /opt/cody/go/go/bin/go

QOPTS := -m 256M

OS := $(shell uname -s)

ifeq ($(OS), OpenBSD)
	ASFLAGS += -nopie
endif

all: d.img go.img

-include $(RFS)

boot: boot.o bootmain.o
	$(LD) --omagic -o $@.elf -m elf_i386 -static -e start -Ttext 0x7c00 $^
	# i want data in the bootloader now too
	@#objcopy -O binary -j .text $@.elf $@.bin
	objcopy -O binary $@.elf $@.bin
	./stamp.py $@.bin
	mv $@.bin $@

bootmain.o: bootmain.c
	# don't use -Os because it rearranges code in .text such that start
	# isn't first. htf do i force ld to put a specific function first in
	# the section?
	@#$(CC) -c $(CFLAGS) -Os $<
	$(CC) -c $(CFLAGS) $<

d.img: main boot
	cat boot main > $@

go.img: boot main.gobin
	cat boot main.gobin > $@

main.gobin: chentry $(GOBIN) main.go
	$(GOBIN) build -o $@ main.go
	ADDR=0x`nm main.gobin |grep _rt0_hack |cut -f1 -d' '`; \
		./chentry $@ $$ADDR

main: main.o
	$(LD) -m elf_i386 -static -e main -Tdata 0x11000 -Ttext 0x20000 -o $@ $<

chentry: chentry.c
	$(CC) $(BASEFLAGS) -o $@ chentry.c

clean:
	rm -f $(OBJS) $(RFS) $@.elf d.img main boot main.gobin go.img

qemu: d.img
	qemu-system-i386 $(QOPTS) -hda d.img

qemu-gdb: d.img
	qemu-system-i386 $(QOPTS) -S -s -hda d.img

gqemu: go.img
	qemu-system-i386 $(QOPTS) -hda go.img

gqemu-gdb: go.img
	qemu-system-i386 $(QOPTS) -S -s -hda go.img

.PHONY: clean qemu gqemu
