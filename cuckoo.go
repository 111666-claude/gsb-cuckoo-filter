// Package cuckoo 实现布谷鸟过滤器。
// 缺陷：两个候选桶算成同一个位置、踢出只能原地打转、装不下时不扩容还会丢元素。
package cuckoo

import (
	"errors"
	"hash/fnv"
)

// BucketSize 是每个桶的槽数。
const BucketSize = 4

// MaxKicks 是单次插入允许的最大踢出次数。
const MaxKicks = 500

var (
	// ErrFull 表示装不下。
	ErrFull = errors.New("cuckoo: 装不下，需要扩容")
	// ErrMissing 表示没有这个元素。
	ErrMissing = errors.New("cuckoo: 没有这个元素")
)

// Filter 是过滤器本体。
type Filter struct {
	buckets [][BucketSize]uint16
	count   int
	bits    int
}

// New 建表。
func New(capacity, fingerprintBits int) *Filter {
	if capacity < 1 {
		capacity = 1
	}
	size := capacity / BucketSize
	if size < 1 {
		size = 1
	}
	if fingerprintBits < 2 || fingerprintBits > 16 {
		fingerprintBits = 16
	}
	return &Filter{buckets: make([][BucketSize]uint16, size), bits: fingerprintBits}
}

// Fingerprint 计算指纹。
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

// index1 是第一个候选桶。
func (f *Filter) index1(value uint16) int {
	return int(value) % len(f.buckets)
}

// index2 缺陷：本该是 index1 异或一个独立的哈希，现在与 index1 相同。
func (f *Filter) index2(value uint16) int {
	return f.index1(value)
}

// Insert 插入一个元素。缺陷：踢出时换不出另一个桶，最后把被踢出的指纹丢掉。
func (f *Filter) Insert(item string) error {
	value := Fingerprint(item)
	for _, index := range []int{f.index1(value), f.index2(value)} {
		for slot := 0; slot < BucketSize; slot++ {
			if f.buckets[index][slot] == 0 {
				f.buckets[index][slot] = value
				f.count++
				return nil
			}
		}
	}
	index := f.index1(value)
	current := value
	for step := 0; step < MaxKicks; step++ {
		slot := step % BucketSize
		current, f.buckets[index][slot] = f.buckets[index][slot], current
		index = f.index2(current)
		if current == 0 {
			f.count++
			return nil
		}
	}
	return nil
}

// Lookup 查询。
func (f *Filter) Lookup(item string) bool {
	value := Fingerprint(item)
	for _, index := range []int{f.index1(value), f.index2(value)} {
		for slot := 0; slot < BucketSize; slot++ {
			if f.buckets[index][slot] == value {
				return true
			}
		}
	}
	return false
}

// Delete 删除。
func (f *Filter) Delete(item string) error {
	value := Fingerprint(item)
	for _, index := range []int{f.index1(value), f.index2(value)} {
		for slot := 0; slot < BucketSize; slot++ {
			if f.buckets[index][slot] == value {
				f.buckets[index][slot] = 0
				f.count--
				return nil
			}
		}
	}
	return ErrMissing
}

// Count 是元素个数。
func (f *Filter) Count() int {
	return f.count
}

// Capacity 是总槽数。
func (f *Filter) Capacity() int {
	return len(f.buckets) * BucketSize
}

// Load 是装载率。
func (f *Filter) Load() float64 {
	return float64(f.count) / float64(f.Capacity())
}
