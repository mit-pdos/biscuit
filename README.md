# Biscuit research OS

Biscuit is a monolithic, POSIX-subset operating system kernel in Go for x86-64
CPUs. It was written to study the performance trade-offs of using a high-level
language with garbage collection to implement a kernel with a common style of
architecture. You can find the research paper done on Biscuit at the PDOS
website: https://pdos.csail.mit.edu/publications/.

Biscuit has some important features for getting good application performance:
- Multicore
- Kernel-supported threads
- Journaled FS with concurrent, deferred, and group commit
- Virtual memory for copy-on-write and lazily mapped anonymous/file pages
- TCP/IP stack
- AHCI SATA disk driver
- Intel 10Gb NIC driver

Biscuit also includes a bootloader, a partial libc ("litc"), and some user
space programs, though we could have used GRUB or existing libc
implementations, like musl.

Biscuit's repo is a fork of the Go repo (https://github.com/golang/go).  Nearly
all of Biscuit's code is in biscuit/.

## Install

The root of the repository contains the Go 1.10.1 tools/runtime. Some of
Biscuit's code is modifications to the runtime, mostly in
src/runtime/os_linux.go.

You must build Biscuit's modified Go runtime before building Biscuit:

( cd src/ && ./make.bash )

then run Biscuit:

( cd biscuit/ && make qemu CPUS=2 )

Biscuit should boot, then you can type a command:
ls
