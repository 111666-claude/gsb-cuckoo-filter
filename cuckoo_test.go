package cuckoo

import "testing"

func TestInsertAndLookup(t *testing.T) {
	filter := New(64, 16)
	if err := filter.Insert("alpha"); err != nil {
		t.Fatalf("Insert 出错：%v", err)
	}
	if !filter.Lookup("alpha") {
		t.Fatal("刚插入的元素应该能查到")
	}
	if filter.Lookup("beta") {
		t.Fatal("没插入的元素不该查到")
	}
}

func TestCountAndCapacity(t *testing.T) {
	filter := New(64, 16)
	for index := 0; index < 10; index++ {
		if err := filter.Insert(string(rune('a' + index))); err != nil {
			t.Fatalf("Insert 出错：%v", err)
		}
	}
	if filter.Count() != 10 {
		t.Fatalf("计数不对：%d", filter.Count())
	}
	if filter.Capacity() != 64 {
		t.Fatalf("容量不对：%d", filter.Capacity())
	}
}

func TestDeleteExisting(t *testing.T) {
	filter := New(64, 16)
	filter.Insert("alpha")
	if err := filter.Delete("alpha"); err != nil {
		t.Fatalf("Delete 出错：%v", err)
	}
	if filter.Lookup("alpha") {
		t.Fatal("删掉之后不该查到")
	}
}

func TestDeleteMissing(t *testing.T) {
	filter := New(64, 16)
	if err := filter.Delete("nope"); err != ErrMissing {
		t.Fatalf("删除不存在的元素应该报 ErrMissing：%v", err)
	}
}

func TestFingerprintIsNotZero(t *testing.T) {
	for index := 0; index < 20; index++ {
		if Fingerprint(string(rune('a'+index))) == 0 {
			t.Fatal("指纹不能是 0")
		}
	}
}

func TestEmptyLoad(t *testing.T) {
	if load := New(8, 16).Load(); load != 0 {
		t.Fatalf("空过滤器装载率应该是 0：%v", load)
	}
}
