package fs

import "mem"

type Superblock_t struct {
	Data *mem.Bytepg_t
}

func (sb *Superblock_t) Loglen() int {
	return fieldr(sb.Data, 0)
}

func (sb *Superblock_t) Iorphanblock() int {
	return fieldr(sb.Data, 1)
}

func (sb *Superblock_t) Iorphanlen() int {
	return fieldr(sb.Data, 2)
}

func (sb *Superblock_t) Imaplen() int {
	return fieldr(sb.Data, 3)
}

func (sb *Superblock_t) Freeblock() int {
	return fieldr(sb.Data, 4)
}

func (sb *Superblock_t) Freeblocklen() int {
	return fieldr(sb.Data, 5)
}

func (sb *Superblock_t) Inodelen() int {
	return fieldr(sb.Data, 6)
}

func (sb *Superblock_t) Lastblock() int {
	return fieldr(sb.Data, 7)
}

// writing

func (sb *Superblock_t) SetLoglen(ll int) {
	fieldw(sb.Data, 0, ll)
}

func (sb *Superblock_t) SetIorphanblock(n int) {
	fieldw(sb.Data, 1, n)
}

func (sb *Superblock_t) SetIorphanlen(n int) {
	fieldw(sb.Data, 2, n)
}

func (sb *Superblock_t) SetImaplen(n int) {
	fieldw(sb.Data, 3, n)
}

func (sb *Superblock_t) SetFreeblock(n int) {
	fieldw(sb.Data, 4, n)
}

func (sb *Superblock_t) SetFreeblocklen(n int) {
	fieldw(sb.Data, 5, n)
}

func (sb *Superblock_t) SetInodelen(n int) {
	fieldw(sb.Data, 6, n)
}

func (sb *Superblock_t) SetLastblock(n int) {
	fieldw(sb.Data, 7, n)
}
