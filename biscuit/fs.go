package main

import "fmt"
import "runtime"
import "strings"

// given to us by bootloader, initialized in rt0_go_hack
var fsblock_start int

const NAME_MAX    int = 512

const ROOT_INODE  int = 1

func path_sanitize(path string) []string {
	sp := strings.Split(path, "/")
	nn := []string{}
	for _, s := range sp {
		if s != "" {
			nn = append(nn, s)
		}
	}
	return nn
}

// returns fs device identifier and inode
func fs_open(path []string, flags int, mode int) (int, int) {
	fmt.Printf("fs open %q\n", path)

	dirnode, inode := fs_walk(path)
	if flags & O_CREAT == 0 {
		if inode == -1 {
			return 0, -ENOENT
		}
		return inode, 0
	}

	name := path[len(path) - 1]
	inode = bisfs_create(name, dirnode)

	return inode, 0
}

// returns inode and error
func fs_walk(path []string) (int, int) {
	cinode := ROOT_INODE
	for _, p := range path {
		nexti, err := bisfs_dir_get(cinode, p)
		if err != 0 {
			return 0, err
		}
		cinode = nexti
	}
	return cinode, 0
}

func itobn(ind int) int {
	return fsblock_start + ind
}

func bisfs_create(name string, dirnode int) int {
	return -1
}

func bisfs_dir_get(dnode int, name string) (int, int) {
	bn := itobn(dnode)
	blk := bisblk_t{bc_fetch(bn)}
	inode, err := blk.lookup(name)
	return inode, err
}

type bisblk_t struct {
	data	*[512]byte
}

func (b *bisblk_t) lookup(name string) (int, int) {
	return 0, 0
}

func bc_fetch(blockno int) *[512]byte {
	return nil
}

func init_8259() {
	// the piix3 provides two 8259 compatible pics. the runtime masks all
	// irqs for us.
	outb := func(reg int, val int) {
		runtime.Outb(int32(reg), int32(val))
		cdelay(1)
	}
	pic1 := 0x20
	pic1d := pic1 + 1
	pic2 := 0xa0
	pic2d := pic2 + 1

	runtime.Cli()

	// master pic
	// start icw1: icw4 required
	outb(pic1, 0x11)
	// icw2, int base -- irq # will be added to base, then delivered to cpu
	outb(pic1d, IRQBASE)
	// icw3, cascaded mode
	outb(pic1d, 4)
	// icw4, auto eoi, intel arch mode.
	outb(pic1d, 3)

	// slave pic
	// start icw1: icw4 required
	outb(pic2, 0x11)
	// icw2, int base -- irq # will be added to base, then delivered to cpu
	outb(pic2d, IRQBASE + 8)
	// icw3, slave identification code
	outb(pic2d, 2)
	// icw4, auto eoi, intel arch mode.
	outb(pic2d, 3)

	// ocw3, enable "special mask mode" (??)
	outb(pic1, 0x68)
	// ocw3, read irq register
	outb(pic1, 0x0a)

	// ocw3, enable "special mask mode" (??)
	outb(pic2, 0x68)
	// ocw3, read irq register
	outb(pic2, 0x0a)

	// enable slave 8259
	irq_unmask(2)
	// all irqs go to CPU 0 until redirected

	runtime.Sti()
}

var intmask uint16 	= 0xffff

func irq_unmask(irq int) {
	if irq < 0 || irq > 16 {
		panic("weird irq")
	}
	pic1 := int32(0x20)
	pic1d := pic1 + 1
	pic2 := int32(0xa0)
	pic2d := pic2 + 1
	intmask = intmask & ^(1 << uint(irq))
	dur := int32(intmask)
	runtime.Outb(pic1d, dur)
	runtime.Outb(pic2d, dur >> 8)
}

// use ata pio for fair comparisons against xv6, but i want to use ahci (or
// something) eventually
