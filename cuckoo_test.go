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

// TestKickRelocation 插入数量超过桶的初始槽位，强制走踢出重定位路径，
// 校验被踢出的指纹在另一个候选桶里继续安家，没有元素丢失。
func TestKickRelocation(t *testing.T) {
	filter := New(4, 16)
	for i := 0; i < 13; i++ {
		key := fmt.Sprintf("kick-%d", i)
		if err := filter.Insert(key); err != nil {
			t.Fatalf("Insert(%q) 出错：%v", key, err)
		}
	}
	if filter.Count() != 13 {
		t.Fatalf("计数不对：%d", filter.Count())
	}
	for i := 0; i < 13; i++ {
		key := fmt.Sprintf("kick-%d", i)
		if !filter.Lookup(key) {
			t.Fatalf("踢出重定位后 %q 应该仍能查到", key)
		}
	}
}

// TestGrow 插入量超过初始容量，校验翻倍扩容重建：容量成倍增加、
// 装载率降到 0.9 以下、所有存活元素零假阴性。
func TestGrow(t *testing.T) {
	filter := New(16, 16)
	initial := filter.Capacity()
	for i := 0; i < 100; i++ {
		if err := filter.Insert(fmt.Sprintf("grow-%d", i)); err != nil {
			t.Fatalf("Insert(%d) 出错：%v", i, err)
		}
	}
	if filter.Capacity() <= initial {
		t.Fatalf("扩容后容量应大于初始值 %d，实际 %d", initial, filter.Capacity())
	}
	if filter.Capacity()%initial != 0 {
		t.Fatalf("扩容应成倍翻倍：initial=%d now=%d", initial, filter.Capacity())
	}
	if load := filter.Load(); load > 0.9 {
		t.Fatalf("扩容后装载率应不超过 0.9：%.3f", load)
	}
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("grow-%d", i)
		if !filter.Lookup(key) {
			t.Fatalf("扩容重建后 %q 应该仍能查到", key)
		}
	}
}

// TestDeletePrecise 删除一个元素不能影响指纹或候选桶不同的其他元素；
// 删除后再删同一个元素必须报 ErrMissing。
func TestDeletePrecise(t *testing.T) {
	filter := New(64, 16)
	keys := []string{"alpha", "beta", "gamma", "delta"}
	for _, key := range keys {
		if err := filter.Insert(key); err != nil {
			t.Fatalf("Insert(%q) 出错：%v", key, err)
		}
	}
	if err := filter.Delete("beta"); err != nil {
		t.Fatalf("Delete 出错：%v", err)
	}
	if filter.Lookup("beta") {
		t.Fatal("beta 删除后不该再查到")
	}
	for _, key := range []string{"alpha", "gamma", "delta"} {
		if !filter.Lookup(key) {
			t.Fatalf("删除 beta 不应误删 %q", key)
		}
	}
	if err := filter.Delete("beta"); err != ErrMissing {
		t.Fatalf("重复删除应报 ErrMissing：%v", err)
	}
	if filter.Count() != 3 {
		t.Fatalf("删除后计数应为 3：%d", filter.Count())
	}
}

// TestFalsePositiveRate 1 万个确定不存在的随机元素，误报率必须 ≤ 5%。
func TestFalsePositiveRate(t *testing.T) {
	filter := New(8192, 16)
	rng := rand.New(rand.NewSource(42))
	inserted := make(map[string]bool, 5000)
	for len(inserted) < 5000 {
		key := fmt.Sprintf("present-%d", rng.Int63())
		if !inserted[key] {
			inserted[key] = true
			if err := filter.Insert(key); err != nil {
				t.Fatalf("Insert 出错：%v", err)
			}
		}
	}
	falsePositive := 0
	const queries = 10000
	for i := 0; i < queries; i++ {
		key := fmt.Sprintf("absent-%d", i)
		if inserted[key] {
			continue
		}
		if filter.Lookup(key) {
			falsePositive++
		}
	}
	rate := float64(falsePositive) / float64(queries)
	if rate > 0.05 {
		t.Fatalf("误报率 %.4f 超过 5%%（%d/%d）", rate, falsePositive, queries)
	}
}

// TestNoFalseNegativeVsMap 对照参考实现：用 map 记录全部存活元素，
// 1 万个元素逐条比对 Lookup 结果，差异必须为 0。
func TestNoFalseNegativeVsMap(t *testing.T) {
	filter := New(16, 16)
	reference := make(map[string]bool, 10000)
	for i := 0; i < 10000; i++ {
		key := fmt.Sprintf("elem-%d", i)
		if err := filter.Insert(key); err != nil {
			t.Fatalf("Insert(%d) 出错：%v", i, err)
		}
		reference[key] = true
	}
	for i := 0; i < 10000; i++ {
		key := fmt.Sprintf("elem-%d", i)
		if !filter.Lookup(key) {
			t.Fatalf("假阴性：%q 在参考实现中存在但过滤器查不到", key)
		}
		if filter.Lookup(key) != reference[key] {
			t.Fatalf("%q 与参考实现结果不一致", key)
		}
	}
	if filter.Count() != len(reference) {
		t.Fatalf("Count=%d 与参考实现 %d 不一致", filter.Count(), len(reference))
	}

	for i := 0; i < 5000; i++ {
		key := fmt.Sprintf("elem-%d", i)
		if err := filter.Delete(key); err != nil {
			t.Fatalf("Delete(%q) 出错：%v", key, err)
		}
		delete(reference, key)
	}
	if filter.Count() != len(reference) {
		t.Fatalf("删除后 Count=%d 与参考实现 %d 不一致", filter.Count(), len(reference))
	}
	for key, present := range reference {
		if filter.Lookup(key) != present {
			t.Fatalf("删除后 %q 与参考实现结果不一致", key)
		}
	}
}

// TestDeterministic 同样的插入序列重复执行，容量与全部查询结果必须一致。
func TestDeterministic(t *testing.T) {
	build := func() (*Filter, []bool) {
		filter := New(1024, 16)
		for i := 0; i < 5000; i++ {
			if err := filter.Insert(fmt.Sprintf("det-%d", i)); err != nil {
				t.Fatalf("Insert 出错：%v", err)
			}
		}
		results := make([]bool, 5000)
		for i := range results {
			results[i] = filter.Lookup(fmt.Sprintf("det-%d", i))
		}
		return filter, results
	}
	first, firstResults := build()
	second, secondResults := build()
	if first.Capacity() != second.Capacity() {
		t.Fatalf("容量不确定：%d vs %d", first.Capacity(), second.Capacity())
	}
	for i := range firstResults {
		if firstResults[i] != secondResults[i] {
			t.Fatalf("第 %d 个元素查询结果不确定", i)
		}
	}
}

// Benchmark100kInsertLookup 规模门禁：10 万次插入 + 10 万次查询应在 1 秒内。
func Benchmark100kInsertLookup(b *testing.B) {
	const n = 100000
	for round := 0; round < b.N; round++ {
		filter := New(1024, 16)
		for i := 0; i < n; i++ {
			if err := filter.Insert(fmt.Sprintf("bench-%d", i)); err != nil {
				b.Fatalf("Insert 出错：%v", err)
			}
		}
		for i := 0; i < n; i++ {
			if !filter.Lookup(fmt.Sprintf("bench-%d", i)) {
				b.Fatalf("假阴性：bench-%d", i)
			}
		}
	}
}

// TestBenchmarkWithinOneSecond 直接计时验证 10 万插入 + 10 万查询在 1 秒内完成。
func TestBenchmarkWithinOneSecond(t *testing.T) {
	if testing.Short() {
		t.Skip("短模式跳过规模测试")
	}
	const n = 100000
	filter := New(1024, 16)
	start := time.Now()
	for i := 0; i < n; i++ {
		if err := filter.Insert(fmt.Sprintf("timed-%d", i)); err != nil {
			t.Fatalf("Insert 出错：%v", err)
		}
	}
	for i := 0; i < n; i++ {
		if !filter.Lookup(fmt.Sprintf("timed-%d", i)) {
			t.Fatalf("假阴性：timed-%d", i)
		}
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("10 万插入+查询耗时 %v，超过 1 秒", elapsed)
	}
}
