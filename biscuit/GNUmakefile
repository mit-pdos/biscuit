TOP := .

ASFLAGS := -Wall -Werror -nostdinc -nopie -I$(TOP) -m32
CFLAGS := $(ASFLAGS) -MMD -MP -fno-stack-protector

ASMS := boot.S
CS   := bootmain.c main.c
SRCS := $(ASMS) $(CS)
OBJS := $(patsubst %.S,%.o,$(patsubst %.c,%.o,$(SRCS)))

RFS  := $(patsubst %.c,%.d,$(CS))

all: d.img

-include $(RFS)

boot: boot.o bootmain.o
	$(LD) --omagic -o $@.elf -m elf_i386 -static -e start -Ttext 0x7c00 $^
	objcopy -O binary -j .text $@.elf $@.bin
	./stamp.py $@.bin
	mv $@.bin $@
	rm -f $@.elf

bootmain.o: bootmain.c
	$(CC) -c $(CFLAGS) -Os $<

d.img: main boot
	cat boot main > d.img

main: main.o
	$(LD) -m elf_i386 -static -e main -Ttext 0x1000 -o $@ $<

clean:
	rm -f $(OBJS) $(RFS) $@.elf d.img main boot

qemu: d.img
	qemu-system-i386 -hda d.img

.PHONY: clean
