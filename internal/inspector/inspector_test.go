package inspector

import (
	"testing"
)

func TestParseFree_ModernWithSwap(t *testing.T) {
	out := `               total        used        free      shared  buff/cache   available
Mem:     16573849600  4823449600  8923449600   523449600  2826950400 11226950400
Swap:     2147479552   536870912  1610608640`

	ramUsed, ramTot, swpUsed, swpTot := parseFree(out)
	if ramTot != 16573849600 {
		t.Fatalf("expected ramTot 16573849600, got %d", ramTot)
	}
	if ramUsed != 4823449600 {
		t.Fatalf("expected ramUsed 4823449600, got %d", ramUsed)
	}
	if swpTot != 2147479552 {
		t.Fatalf("expected swpTot 2147479552, got %d", swpTot)
	}
	if swpUsed != 536870912 {
		t.Fatalf("expected swpUsed 536870912, got %d", swpUsed)
	}
}

func TestParseFree_WithoutSwap(t *testing.T) {
	out := `Mem: 16777216000 8388608000 8388608000`
	ramUsed, ramTot, swpUsed, swpTot := parseFree(out)
	if ramTot != 16777216000 || ramUsed != 8388608000 {
		t.Fatalf("unexpected ram values: used=%d, tot=%d", ramUsed, ramTot)
	}
	if swpTot != 0 || swpUsed != 0 {
		t.Fatalf("expected 0 swap, got used=%d, tot=%d", swpUsed, swpTot)
	}
}

func TestParseFree_LegacyBuffersCache(t *testing.T) {
	out := `             total       used       free     shared    buffers     cached
Mem:       1019624     648160     371464          0      68280     265432
-/+ buffers/cache:     314448     705176
Swap:      2097144          0    2097144`

	ramUsed, ramTot, swpUsed, swpTot := parseFree(out)
	if ramTot != 1019624 {
		t.Fatalf("expected ramTot 1019624, got %d", ramTot)
	}
	if ramUsed != 314448 {
		t.Fatalf("expected ramUsed 314448, got %d", ramUsed)
	}
	if swpTot != 2097144 || swpUsed != 0 {
		t.Fatalf("expected swap: used=0, tot=2097144, got used=%d, tot=%d", swpUsed, swpTot)
	}
}
