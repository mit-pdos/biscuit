## go os 调试过程

### go源码编译
```
$ https://github.com/fengpf/biscuit.git
$ git checkout biscuit/dev
$ cd biscuit/src

$ GOROOT_BOOTSTRAP=./ GO_GCFLAGS="-N -l" ./make.bash
```


### 设置 GOPATH & GOROOT

```
$ export GOPATH=../biscuit
$ GOROOT=../ && export PATH=$GOROOT/bin:$PATH
```


### 系统启动
```
$ cd ../biscuit
$ GOPATH=$(pwd) make qemu CPUS=2
```


### qemu 启动报错
qemu-system-x86_64: cannot set up guest memory 'pc.ram': Cannot allocate memory

处理办法: 分配的内存大于实际的内存，重新分配小一点的内存便可

