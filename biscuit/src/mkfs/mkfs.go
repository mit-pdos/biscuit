package main

import "os"
import "fmt"

import "common"
import "fs"
import "ufs"

func main() {
	if len(os.Args) < 5 {
		fmt.Printf("Usage: mkfs <bootimage> <kernel image> <output image> <skel dir>\n")
		os.Exit(1)
	}

	//boot := os.Args[1]
	//kernel := os.Args[2]
	image := os.Args[3]
	//skeldir := os.Args[4]
	fmt.Printf("mkfs %s\n", image)
	mkDisk(image)
	fs := ufs.BootFS(image)
	st, err := fs.Stat("/")
	if err != 0 {
		fmt.Printf("not a valid fs: no root inode\n")
		os.Exit(1)
	}
	fmt.Printf("root inode %v\n", st)
	dir, err := fs.Ls("/")
	fmt.Printf("root %v\n", dir)
	if err != 0 {
		fmt.Printf("not a valid fs: no root dir\n")
		os.Exit(1)
	}
	ufs.ShutdownFS(fs)
}
