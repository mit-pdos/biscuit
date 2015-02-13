package main

import "fmt"
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
