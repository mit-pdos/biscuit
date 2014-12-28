TOP := .

BASEFLAGS := -Wall -Werror -MMD -MP -O -g
ASFLAGS := $(BASEFLAGS) -nostdinc -I$(TOP) -m32
CFLAGS := $(BASEFLAGS) -nostdinc -I$(TOP) -fno-stack-protector -fno-builtin

ASMS := boot.S
CS   := bootmain.c main.c chentry.c
SRCS := $(ASMS) $(CS)
OBJS := $(patsubst %.S,%.o,$(patsubst %.c,%.o,$(SRCS)))

RFS  := $(patsubst %.c,%.d,$(CS))

GOBIN := ../bin/go

QOPTS := -m 256M

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

main.gobin: chentry $(GOBIN) main.go
	$(GOBIN) build -o $@ main.go
	ADDR=0x`nm main.gobin |grep _rt0_hack |cut -f1 -d' '`; \
		if test "$$ADDR" = "0x"; then echo no _rt0_hack; false; \
		else ./chentry $@ $$ADDR; fi \

main: main.o
	$(LD) -static -e main -o $@ $<

chentry: chentry.c
	$(CC) $(BASEFLAGS) -o $@ chentry.c

clean:
	rm -f $(OBJS) $(RFS) boot.elf d.img main boot main.gobin go.img chentry

qemu: d.img
	qemu-system-x86_64 $(QOPTS) -hda d.img

qemu-gdb: d.img
	qemu-system-x86_64 $(QOPTS) -S -s -hda d.img

gqemu: go.img
	qemu-system-x86_64 $(QOPTS) -hda go.img

gqemu-gdb: go.img
	qemu-system-x86_64 $(QOPTS) -S -s -hda go.img

.PHONY: clean qemu gqemu
