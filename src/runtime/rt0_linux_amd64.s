// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

#include "textflag.h"

TEXT _rt0_amd64_linux(SB),NOSPLIT,$-8
	LEAQ	8(SP), SI // argv
	MOVQ	0(SP), DI // argc
	MOVQ	$main(SB), AX
	CALL	AX
	MOVL	$1, 0	// abort
	CALL	_rt0_hack(SB)
	INT	$3

TEXT main(SB),NOSPLIT,$-8
	MOVQ	$runtime·rt0_go(SB), AX
	JMP	AX

TEXT _rt0_hack(SB),NOSPLIT,$0
	CALL	runtime·rt0_go_hack(SB)
	INT	$3
