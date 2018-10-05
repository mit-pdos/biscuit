# Biscuit research OS

Biscuit's repo is a fork of the Go repo (https://github.com/golang/go).  Nearly
all of Biscuit's code is in biscuit/.

## Install

The root of the repository contains the Go 1.11.1 tools/runtime. Some of
Biscuit's code is modifications to the runtime, mostly in
src/runtime/os_linux.go.

The vast majority of Biscuit's code is in biscuit/

You must build Biscuit's modified Go runtime before building Biscuit:

( cd src/ && ./make.bash )

then run Biscuit:

( cd biscuit/ && make qemu CPUS=2 )

Biscuit should boot, then you can type a command:
ls
