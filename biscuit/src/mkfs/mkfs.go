package main

import "os"
import "fmt"
import "strings"
import "path/filepath"

import "common"
import "ufs"

func copydata(src string, fs *ufs.Ufs_t, dst string) {
	s, err := os.Open(src)
	if err != nil {
		panic(err)
	}
	b := make([]byte, common.BSIZE)
	for {
		n, err := s.Read(b)
		if err != nil {
			return
		}
		if n == 0 {
			return
		}
		buf := ufs.MkBuf(b)
		fs.Append(dst, buf)
	}
}

func addfiles(fs *ufs.Ufs_t, skeldir string) {
	err := filepath.Walk(skeldir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			fmt.Printf("prevent panic by handling failure accessing a path %q: %v\n", skeldir, err)
			return err
		}
		p := strings.TrimPrefix(path, skeldir)
		if p == "" {
			return nil
		}
		if info.IsDir() {
			e := fs.MkDir(p)
			if e != 0 {
				fmt.Printf("failed to create dir %v\n", p)
			}

		} else {
			e := fs.MkFile(p, nil)
			if e != 0 {
				fmt.Printf("failed to create file %v\n", p)
			}
			copydata(path, fs, p)
		}
		return nil
	})

	if err != nil {
		fmt.Printf("error walking the path %q: %v\n", skeldir, err)
		os.Exit(1)
	}
}

func main() {
	if len(os.Args) < 5 {
		fmt.Printf("Usage: mkfs <bootimage> <kernel image> <output image> <skel dir>\n")
		os.Exit(1)
	}

	//boot := os.Args[1]
	//kernel := os.Args[2]
	image := os.Args[3]
	skeldir := os.Args[4]

	ufs.MkDisk(image)

	fs := ufs.BootFS(image)
	_, err := fs.Stat("/")
	if err != 0 {
		fmt.Printf("not a valid fs: no root inode\n")
		os.Exit(1)
	}
	// fmt.Printf("root inode %v\n", st)

	addfiles(fs, skeldir)

	// dir, err := fs.Ls("/")
	// if err != 0 {
	// 	fmt.Printf("not a valid fs: no root dir\n")
	// 	os.Exit(1)
	// }
	// for k, v := range dir {
	//	fmt.Printf("%v %v\n", k, v)
	//}

	ufs.ShutdownFS(fs)
}
