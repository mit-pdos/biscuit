package main

import "fmt"

// returns fs device identifier and inode
func fs_open(path string, flags int, mode int) (int, int) {
	fmt.Printf("fs open [%v]\n", path)

	return -1, -1
}
