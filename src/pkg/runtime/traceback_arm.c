// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

#include "runtime.h"
#include "arch_GOARCH.h"
#include "malloc.h"

void runtime·deferproc(void);
void runtime·newproc(void);
void runtime·newstack(void);
void runtime·morestack(void);
void runtime·sigpanic(void);
void _div(void);
void _mod(void);
void _divu(void);
void _modu(void);

int32
runtime·gentraceback(byte *pc0, byte *sp, byte *lr0, G *g, int32 skip, uintptr *pcbuf, int32 max)
{
	int32 i, n, iter;
	uintptr pc, lr, tracepc, x;
	byte *fp, *p;
	bool waspanic;
	Stktop *stk;
	Func *f;
	
	pc = (uintptr)pc0;
	lr = (uintptr)lr0;
	fp = nil;
	waspanic = false;

	// If the PC is goexit, the goroutine hasn't started yet.
	if(pc == (uintptr)runtime·goexit) {
		pc = (uintptr)g->entry;
		lr = (uintptr)runtime·goexit;
	}

	// If the PC is zero, it's likely a nil function call.
	// Start in the caller's frame.
	if(pc == 0) {
		pc = lr;
		lr = 0;
	}

	n = 0;
	stk = (Stktop*)g->stackbase;
	for(iter = 0; iter < 100 && n < max; iter++) {	// iter avoids looping forever
		// Typically:
		//	pc is the PC of the running function.
		//	sp is the stack pointer at that program counter.
		//	fp is the frame pointer (caller's stack pointer) at that program counter, or nil if unknown.
		//	stk is the stack containing sp.
		//	The caller's program counter is lr, unless lr is zero, in which case it is *(uintptr*)sp.
		
		if(pc == (uintptr)runtime·lessstack) {
			// Hit top of stack segment.  Unwind to next segment.
			pc = (uintptr)stk->gobuf.pc;
			sp = (byte*)stk->gobuf.sp;
			lr = 0;
			fp = nil;
			if(pcbuf == nil)
				runtime·printf("----- stack segment boundary -----\n");
			stk = (Stktop*)stk->stackbase;
			continue;
		}
		
		if(pc <= 0x1000 || (f = runtime·findfunc(pc)) == nil) {
			// Dangerous, but worthwhile: see if this is a closure by
			// decoding the instruction stream.
			//
			// We check p < p+4 to avoid wrapping and faulting if
			// we have lost track of where we are.
			p = (byte*)pc;
			if((pc&3) == 0 && p < p+4 &&
			   runtime·mheap.arena_start < p &&
			   p+4 < runtime·mheap.arena_used) {
			   	x = *(uintptr*)p;
				if((x&0xfffff000) == 0xe49df000) {
					// End of closure:
					// MOVW.P frame(R13), R15
					pc = *(uintptr*)sp;
					lr = 0;
					sp += x & 0xfff;
					fp = nil;
					continue;
				}
				if((x&0xfffff000) == 0xe52de000 && lr == (uintptr)runtime·goexit) {
					// Beginning of closure.
					// Closure at top of stack, not yet started.
					p += 5*4;
					if((x&0xfff) != 4) {
						// argument copying
						p += 7*4;
					}
					if((byte*)pc < p && p < p+4 && p+4 < runtime·mheap.arena_used) {
						pc = *(uintptr*)p;
						fp = nil;
						continue;
					}
				}
			}
			break;
		}
		
		// Found an actual function.
		if(lr == 0)
			lr = *(uintptr*)sp;
		if(fp == nil) {
			fp = sp;
			if(pc > f->entry && f->frame >= 0)
				fp += f->frame;
		}

		if(skip > 0)
			skip--;
		else if(pcbuf != nil)
			pcbuf[n++] = pc;
		else {
			if(runtime·showframe(f)) {
				// Print during crash.
				//	main(0x1, 0x2, 0x3)
				//		/home/rsc/go/src/runtime/x.go:23 +0xf
				tracepc = pc;	// back up to CALL instruction for funcline.
				if(n > 0 && pc > f->entry && !waspanic)
					tracepc -= sizeof(uintptr);
				runtime·printf("%S(", f->name);
				for(i = 0; i < f->args; i++) {
					if(i != 0)
						runtime·prints(", ");
					runtime·printhex(((uintptr*)fp)[1+i]);
					if(i >= 4) {
						runtime·prints(", ...");
						break;
					}
				}
				runtime·prints(")\n");
				runtime·printf("\t%S:%d", f->src, runtime·funcline(f, tracepc));
				if(pc > f->entry)
					runtime·printf(" +%p", (uintptr)(pc - f->entry));
				runtime·printf("\n");
			}
			n++;
		}
		
		waspanic = f->entry == (uintptr)runtime·sigpanic;

		if(pcbuf == nil && f->entry == (uintptr)runtime·newstack && g == m->g0) {
			runtime·printf("----- newstack called from goroutine %D -----\n", m->curg->goid);
			pc = (uintptr)m->morepc;
			sp = (byte*)m->moreargp - sizeof(void*);
			lr = (uintptr)m->morebuf.pc;
			fp = (byte*)m->morebuf.sp;
			g = m->curg;
			stk = (Stktop*)g->stackbase;
			continue;
		}
		
		if(pcbuf == nil && f->entry == (uintptr)runtime·lessstack && g == m->g0) {
			runtime·printf("----- lessstack called from goroutine %D -----\n", m->curg->goid);
			g = m->curg;
			stk = (Stktop*)g->stackbase;
			sp = (byte*)stk->gobuf.sp;
			pc = (uintptr)stk->gobuf.pc;
			fp = nil;
			lr = 0;
			continue;
		}	
		
		// Unwind to next frame.
		pc = lr;
		lr = 0;
		sp = fp;
		fp = nil;
		
		// If this was div or divu or mod or modu, the caller had
		// an extra 8 bytes on its stack.  Adjust sp.
		if(f->entry == (uintptr)_div || f->entry == (uintptr)_divu || f->entry == (uintptr)_mod || f->entry == (uintptr)_modu)
			sp += 8;
		
		// If this was deferproc or newproc, the caller had an extra 12.
		if(f->entry == (uintptr)runtime·deferproc || f->entry == (uintptr)runtime·newproc)
			sp += 12;
	}
	
	if(pcbuf == nil && (pc = g->gopc) != 0 && (f = runtime·findfunc(pc)) != nil && g->goid != 1) {
		runtime·printf("created by %S\n", f->name);
		tracepc = pc;	// back up to CALL instruction for funcline.
		if(n > 0 && pc > f->entry)
			tracepc -= sizeof(uintptr);
		runtime·printf("\t%S:%d", f->src, runtime·funcline(f, tracepc));
		if(pc > f->entry)
			runtime·printf(" +%p", (uintptr)(pc - f->entry));
		runtime·printf("\n");
	}

	return n;		
}

void
runtime·traceback(byte *pc0, byte *sp, byte *lr, G *g)
{
	runtime·gentraceback(pc0, sp, lr, g, 0, nil, 100);
}

// func caller(n int) (pc uintptr, file string, line int, ok bool)
int32
runtime·callers(int32 skip, uintptr *pcbuf, int32 m)
{
	byte *pc, *sp;
	
	sp = runtime·getcallersp(&skip);
	pc = runtime·getcallerpc(&skip);

	return runtime·gentraceback(pc, sp, 0, g, skip, pcbuf, m);
}
