// Package cuckoo 实现布谷鸟过滤器：两个不同的候选桶 + 指纹踢出重定位 + 翻倍扩容。
package cuckoo

import (
	"errors"
	"hash/fnv"
	"math/rand"
)

// BucketSize 是每个桶的槽数。
const BucketSize = 4

// MaxKicks 是单次插入允许的最大踢出次数。
const MaxKicks = 500

// maxGrowths 限制翻倍次数，超过即认为真的插不进去。
const maxGrowths = 30

// fixedSeed 固定随机源，保证同一输入重复执行踢出路径完全一致。
const fixedSeed int64 = 1

var (
	// ErrFull 表示持续扩容后仍然插不进去。
	ErrFull = errors.New("cuckoo: 装不下，需要扩容")
	// ErrMissing 表示没有这个元素。
	ErrMissing = errors.New("cuckoo: 没有这个元素")
)

// Filter 是过滤器本体。
// items 与 itemPos 记录存活元素，供删除（O(1)）与扩容重建使用；
// 大小随元素数线性增长，因此整体内存仍是 O(容量)。
type Filter struct {
	buckets [][BucketSize]uint16
	items   []string
	itemPos map[string]int
	count   int
	mask    uint16
	rng     *rand.Rand
}

// New 建表，桶数向上取整为 2 的幂，便于用异或定位另一个候选桶。
func New(capacity, fingerprintBits int) *Filter {
	if capacity < 1 {
		capacity = 1
	}
	size := capacity / BucketSize
	if size < 1 {
		size = 1
	}
	size = nextPowerOfTwo(size)
	if size < 2 {
		size = 2
	}
	if fingerprintBits < 2 || fingerprintBits > 16 {
		fingerprintBits = 16
	}
	return &Filter{
		buckets: make([][BucketSize]uint16, size),
		items:   make([]string, 0, capacity),
		itemPos: make(map[string]int, capacity),
		mask:    uint16(1)<<uint(fingerprintBits) - 1,
		rng:     rand.New(rand.NewSource(fixedSeed)),
	}
}

// nextPowerOfTwo 返回不小于 n 的最小 2 的幂。
func nextPowerOfTwo(n int) int {
	power := 1
	for power < n {
		power <<= 1
	}
	return power
}

// Fingerprint 用 FNV-1a 计算 16 位指纹，0 是占位符不允许出现。
func Fingerprint(item string) uint16 {
	hasher := fnv.New32a()
	hasher.Write([]byte(item))
	value := hasher.Sum32()
	value ^= value >> 16
	value &= 0xFFFF
	if value == 0 {
		value = 1
	}
	return uint16(value)
}

// fingerprint 按配置位数截断指纹，截断后为 0 时记为 1。
func (f *Filter) fingerprint(item string) uint16 {
	value := Fingerprint(item) & f.mask
	if value == 0 {
		value = 1
	}
	return value
}

// index1 是第一个候选桶，用与指纹独立的 FNV-1 哈希。
func (f *Filter) index1(item string) int {
	hasher := fnv.New32()
	hasher.Write([]byte(item))
	return int(hasher.Sum32() % uint32(len(f.buckets)))
}

// alternate 由当前桶和指纹算出另一个候选桶。
// 该运算是对称的：alternate(alternate(i, fp), fp) == i，
// 因此被踢出的指纹总能回到它原来的桶，重定位不会丢元素。
func (f *Filter) alternate(index int, fp uint16) int {
	hasher := fnv.New32a()
	var bytes [2]byte
	bytes[0] = byte(fp)
	bytes[1] = byte(fp >> 8)
	hasher.Write(bytes[:])
	offset := int(hasher.Sum32() & uint32(len(f.buckets)-1))
	if offset == 0 {
		offset = 1
	}
	return index ^ offset
}

// Insert 插入一个元素。踢出失败时翻倍扩容重建后重试，真的插不进去才返回 ErrFull。
func (f *Filter) Insert(item string) error {
	if _, exists := f.itemPos[item]; exists {
		return nil
	}
	for attempt := 0; ; attempt++ {
		if f.place(item) {
			pos := len(f.items)
			f.itemPos[item] = pos
			f.items = append(f.items, item)
			f.count++
			return nil
		}
		if attempt >= maxGrowths || !f.grow() {
			return ErrFull
		}
	}
}

// place 尝试把元素放进它的两个候选桶；放不下则踢出重定位，最多 MaxKicks 次。
func (f *Filter) place(item string) bool {
	fp := f.fingerprint(item)
	index1 := f.index1(item)
	index2 := f.alternate(index1, fp)

	for _, index := range [2]int{index1, index2} {
		for slot := 0; slot < BucketSize; slot++ {
			if f.buckets[index][slot] == 0 {
				f.buckets[index][slot] = fp
				return true
			}
		}
	}

	index := [2]int{index1, index2}[f.rng.Intn(2)]
	for step := 0; step < MaxKicks; step++ {
		slot := f.rng.Intn(BucketSize)
		evicted := f.buckets[index][slot]
		f.buckets[index][slot] = fp
		fp = evicted
		index = f.alternate(index, fp)
		for candidate := 0; candidate < BucketSize; candidate++ {
			if f.buckets[index][candidate] == 0 {
				f.buckets[index][candidate] = fp
				return true
			}
		}
	}
	return false
}

// grow 把桶数翻倍，并用存活元素重建整张表。
func (f *Filter) grow() bool {
	oldBuckets := f.buckets
	newSize := len(oldBuckets) * 2
	newFilter := &Filter{
		buckets: make([][BucketSize]uint16, newSize),
		mask:    f.mask,
		rng:     f.rng,
	}

	for _, item := range f.items {
		if !newFilter.place(item) {
			return false
		}
	}
	f.buckets = newFilter.buckets
	return true
}

// Lookup 查询元素：指纹出现在任一候选桶即认为存在。
func (f *Filter) Lookup(item string) bool {
	fp := f.fingerprint(item)
	index1 := f.index1(item)
	index2 := f.alternate(index1, fp)
	for _, index := range [2]int{index1, index2} {
		for slot := 0; slot < BucketSize; slot++ {
			if f.buckets[index][slot] == fp {
				return true
			}
		}
	}
	return false
}

// Delete 只清指纹与候选桶都匹配的槽；元素不存在返回 ErrMissing。
func (f *Filter) Delete(item string) error {
	pos, exists := f.itemPos[item]
	if !exists {
		return ErrMissing
	}
	fp := f.fingerprint(item)
	index1 := f.index1(item)
	index2 := f.alternate(index1, fp)
	for _, index := range [2]int{index1, index2} {
		for slot := 0; slot < BucketSize; slot++ {
			if f.buckets[index][slot] == fp {
				f.buckets[index][slot] = 0
				f.count--
				last := len(f.items) - 1
				moved := f.items[last]
				f.items[pos] = moved
				f.items = f.items[:last]
				if pos != last {
					f.itemPos[moved] = pos
				}
				delete(f.itemPos, item)
				return nil
			}
		}
	}
	return ErrMissing
}

// Count 返回存活元素数。
func (f *Filter) Count() int {
	return f.count
}

// Capacity 返回总槽数。
func (f *Filter) Capacity() int {
	return len(f.buckets) * BucketSize
}

// Load 返回装载率。
func (f *Filter) Load() float64 {
	return float64(f.count) / float64(f.Capacity())
}
