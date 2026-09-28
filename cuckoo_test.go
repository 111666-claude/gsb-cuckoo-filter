package cuckoo

import (
	"fmt"
	"math/rand"
	"testing"
	"time"
)

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
	if filter.Count() != 0 {
		t.Fatalf("删除后计数应为 0：%d", filter.Count())
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

// TestCandidateBucketsDiffer 验证两个候选桶一定是不同的桶。
func TestCandidateBucketsDiffer(t *testing.T) {
	filter := New(1024, 16)
	for i := 0; i < 10000; i++ {
		item := fmt.Sprintf("key-%d", i)
		fp := filter.fingerprint(item)
		i1 := filter.index1(item)
		i2 := filter.altIndex(i1, fp)
		if i1 == i2 {
			t.Fatalf("%s 的两个候选桶相同：%d", item, i1)
		}
		if filter.altIndex(i2, fp) != i1 {
			t.Fatalf("候选桶不对称：%d -> %d -> %d", i1, i2, filter.altIndex(i2, fp))
		}
	}
}

// TestKicksRelocate 用小容量高负载逼出踢出重定位，零假阴性。
func TestKicksRelocate(t *testing.T) {
	filter := New(16, 16) // 初始仅 4 个桶
	const n = 64          // 必然触发大量踢出
	for i := 0; i < n; i++ {
		if err := filter.Insert(fmt.Sprintf("key-%d", i)); err != nil {
			t.Fatalf("Insert(%d) 出错：%v", i, err)
		}
	}
	if filter.Count() != n {
		t.Fatalf("踢出期间丢元素：count=%d want=%d", filter.Count(), n)
	}
	for i := 0; i < n; i++ {
		if !filter.Lookup(fmt.Sprintf("key-%d", i)) {
			t.Fatalf("插入成功的 key-%d 查不到（假阴性）", i)
		}
	}
}

// TestGrow 初始容量装不下时必须翻倍扩容，且扩容后不丢元素。
func TestGrow(t *testing.T) {
	filter := New(16, 16)
	const n = 100
	for i := 0; i < n; i++ {
		if err := filter.Insert(fmt.Sprintf("key-%d", i)); err != nil {
			t.Fatalf("Insert(%d) 出错：%v", i, err)
		}
	}
	if filter.Count() != n {
		t.Fatalf("扩容后计数不对：%d", filter.Count())
	}
	if filter.Capacity() <= 16 {
		t.Fatalf("应该已经扩容：capacity=%d", filter.Capacity())
	}
	for i := 0; i < n; i++ {
		if !filter.Lookup(fmt.Sprintf("key-%d", i)) {
			t.Fatalf("扩容后 key-%d 查不到", i)
		}
	}
}

// TestHeavyLoadZeroFalseNegative 复现 README 的 1024/5000 场景。
func TestHeavyLoadZeroFalseNegative(t *testing.T) {
	filter := New(1024, 16)
	const n = 5000
	for i := 0; i < n; i++ {
		if err := filter.Insert(fmt.Sprintf("key-%d", i)); err != nil {
			t.Fatalf("Insert(%d) 出错：%v", i, err)
		}
	}
	if filter.Count() != n {
		t.Fatalf("count=%d want=%d", filter.Count(), n)
	}
	falseNegative := 0
	for i := 0; i < n; i++ {
		if !filter.Lookup(fmt.Sprintf("key-%d", i)) {
			falseNegative++
		}
	}
	if falseNegative != 0 {
		t.Fatalf("假阴性 %d，必须为 0", falseNegative)
	}
}

// TestDeletePrecise 删除只影响指纹与候选桶都匹配的槽，不能误删别人。
func TestDeletePrecise(t *testing.T) {
	filter := New(64, 16)
	const n = 100
	for i := 0; i < n; i++ {
		if err := filter.Insert(fmt.Sprintf("key-%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < n; i += 2 {
		if err := filter.Delete(fmt.Sprintf("key-%d", i)); err != nil {
			t.Fatalf("Delete(%d) 出错：%v", i, err)
		}
	}
	if filter.Count() != n/2 {
		t.Fatalf("删除后计数应为 %d：%d", n/2, filter.Count())
	}
	for i := 0; i < n; i++ {
		found := filter.Lookup(fmt.Sprintf("key-%d", i))
		if i%2 == 0 && found {
			t.Fatalf("key-%d 已删除却仍查到（误删后又被误报掩盖也算数据损坏）", i)
		}
		if i%2 == 1 && !found {
			t.Fatalf("key-%d 没删却查不到（被误删）", i)
		}
	}
	if err := filter.Delete("key-0"); err != ErrMissing {
		t.Fatalf("重复删除已删元素应报 ErrMissing：%v", err)
	}
}

// TestFalsePositiveRate 1 万个确定不在表里的随机元素，误报率 ≤ 5%。
func TestFalsePositiveRate(t *testing.T) {
	filter := New(16384, 16)
	const n = 5000
	for i := 0; i < n; i++ {
		if err := filter.Insert(fmt.Sprintf("member-%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	rng := rand.New(rand.NewSource(42))
	const samples = 10000
	falsePositive := 0
	for i := 0; i < samples; i++ {
		candidate := fmt.Sprintf("absent-%d", rng.Int63())
		if filter.Lookup(candidate) {
			falsePositive++
		}
	}
	rate := float64(falsePositive) / samples
	if rate > 0.05 {
		t.Fatalf("误报率 %.4f 超过 5%%（false_positive=%d）", rate, falsePositive)
	}
	t.Logf("误报率 %.4f（false_positive=%d）", rate, falsePositive)
}

// TestReferenceComparison 与 map 参考实现逐条比对 1 万个元素，差异必须为 0。
func TestReferenceComparison(t *testing.T) {
	filter := New(1024, 16)
	reference := make(map[string]bool)
	rng := rand.New(rand.NewSource(7))
	const n = 10000
	for i := 0; i < n; i++ {
		item := fmt.Sprintf("ref-%d", rng.Intn(3*n))
		if !reference[item] {
			if err := filter.Insert(item); err != nil {
				t.Fatal(err)
			}
			reference[item] = true
		}
	}
	if filter.Count() != len(reference) {
		t.Fatalf("count=%d 参考集合=%d", filter.Count(), len(reference))
	}
	for item := range reference {
		if !filter.Lookup(item) {
			t.Fatalf("参考集合中的 %s 在过滤器里查不到", item)
		}
	}
	check := rand.New(rand.NewSource(99))
	mismatches := 0
	for i := 0; i < n; i++ {
		item := fmt.Sprintf("chk-%d", check.Intn(5*n))
		want := reference[item]
		// 过滤器只可能对非成员产生误报；成员必须 100% 命中。
		if want && !filter.Lookup(item) {
			mismatches++
		}
	}
	if mismatches != 0 {
		t.Fatalf("与参考实现差异 %d，必须为 0", mismatches)
	}
}

// TestDeterministic 同一输入重复执行，扩容/踢出后的结果完全一致。
func TestDeterministic(t *testing.T) {
	build := func() []bool {
		filter := New(16, 16)
		for i := 0; i < 200; i++ {
			if err := filter.Insert(fmt.Sprintf("key-%d", i)); err != nil {
				t.Fatal(err)
			}
		}
		results := make([]bool, 400)
		for i := range results {
			results[i] = filter.Lookup(fmt.Sprintf("key-%d", i))
		}
		return results
	}
	first := build()
	second := build()
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("key-%d 两次运行结果不一致：%v vs %v", i, first[i], second[i])
		}
	}
}

// TestScale 10 万次插入 + 10 万次查询需在 1 秒内完成。
func TestScale(t *testing.T) {
	filter := New(1<<17, 16)
	const n = 100000
	start := time.Now()
	for i := 0; i < n; i++ {
		if err := filter.Insert(fmt.Sprintf("scale-%d", i)); err != nil {
			t.Fatalf("Insert(%d) 出错：%v", i, err)
		}
	}
	for i := 0; i < n; i++ {
		if !filter.Lookup(fmt.Sprintf("scale-%d", i)) {
			t.Fatalf("scale-%d 假阴性", i)
		}
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("10 万插入 + 10 万查询耗时 %v，超过 1 秒", elapsed)
	}
}
