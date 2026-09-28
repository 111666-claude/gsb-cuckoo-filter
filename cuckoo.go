// Package cuckoo 实现布谷鸟过滤器：两个候选桶 + 指纹踢出 + 翻倍扩容。
package cuckoo

import (
	"encoding/binary"
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

// fixedSeed 让同一输入重复执行得到完全相同的结果。
const fixedSeed int64 = 1

var (
	// ErrFull 表示装不下。
	ErrFull = errors.New("cuckoo: 装不下，需要扩容")
	// ErrMissing 表示没有这个元素。
	ErrMissing = errors.New("cuckoo: 没有这个元素")
)

// Filter 是过滤器本体。items 保存存活元素，扩容时据此按新桶数重算候选桶。
type Filter struct {
	buckets [][BucketSize]uint16
	items   []string
	count   int
	mask    uint16
	rng     *rand.Rand
}

// New 建表，桶数向上取整为 2 的幂以保证异或下标不越界。
func New(capacity, fingerprintBits int) *Filter {
	if capacity < 1 {
		capacity = 1
	}
	size := capacity / BucketSize
	if size < 1 {
		size = 1
	}
	size = nextPowerOfTwo(size)
	if fingerprintBits < 2 || fingerprintBits > 16 {
		fingerprintBits = 16
	}
	return &Filter{
		buckets: make([][BucketSize]uint16, size),
		mask:    uint16(1)<<uint(fingerprintBits) - 1,
		rng:     rand.New(rand.NewSource(fixedSeed)),
	}
}

func nextPowerOfTwo(n int) int {
	power := 1
	for power < n {
		power <<= 1
	}
	return power
}

// Fingerprint 用 FNV-1a 计算 16 位指纹，0 占位符不允许出现。
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

// fingerprint 按配置位数截断指纹。
func (f *Filter) fingerprint(item string) uint16 {
	value := Fingerprint(item) & f.mask
	if value == 0 {
		value = 1
	}
	return value
}

// index1 是第一个候选桶，用与指纹不同的 FNV-1 哈希。
func (f *Filter) index1(item string) int {
	hasher := fnv.New32()
	hasher.Write([]byte(item))
	return int(hasher.Sum32()) & (len(f.buckets) - 1)
}

// altIndex 计算指纹在已知一个候选桶时的另一个候选桶：
// index2 = index1 ^ (指纹的另一个哈希 % 桶数)，偏移非零故两桶必不同。
func (f *Filter) altIndex(index int, fingerprint uint16) int {
	var buffer [2]byte
	binary.LittleEndian.PutUint16(buffer[:], fingerprint)
	hasher := fnv.New32a()
	hasher.Write(buffer[:])
	offset := int(hasher.Sum32() % uint32(len(f.buckets)))
	if offset == 0 {
		offset = 1
	}
	return index ^ offset
}

// Insert 插入一个元素；桶满时踢出重定位，踢出失败则翻倍扩容重建后再试。
func (f *Filter) Insert(item string) error {
	fp := f.fingerprint(item)
	if placed, _, _ := f.place(fp, f.index1(item)); placed {
		f.items = append(f.items, item)
		return nil
	}
	// place 返回时被踢出的指纹仍在它手里，没有丢；grow 会把存活元素全部重建。
	if err := f.grow(); err != nil {
		return err
	}
	if placed, _, _ := f.place(fp, f.index1(item)); placed {
		f.items = append(f.items, item)
		return nil
	}
	return ErrFull
}

// place 尝试把指纹放进 i1/alt(i1)；冲突时踢出重定位，最多 MaxKicks 次。
// 失败时返回手里最后一个被踢出的指纹，表内元素一个不丢，调用方负责扩容。
func (f *Filter) place(fp uint16, i1 int) (placed bool, evicted uint16, evictedIndex int) {
	if slot := f.findEmpty(i1); slot >= 0 {
		f.buckets[i1][slot] = fp
		f.count++
		return true, 0, 0
	}
	i2 := f.altIndex(i1, fp)
	if slot := f.findEmpty(i2); slot >= 0 {
		f.buckets[i2][slot] = fp
		f.count++
		return true, 0, 0
	}

	index := i1
	if f.rng.Intn(2) == 1 {
		index = i2
	}
	current := fp
	for step := 0; step < MaxKicks; step++ {
		slot := f.rng.Intn(BucketSize)
		current, f.buckets[index][slot] = f.buckets[index][slot], current
		index = f.altIndex(index, current)
		if free := f.findEmpty(index); free >= 0 {
			f.buckets[index][free] = current
			f.count++
			return true, 0, 0
		}
	}
	return false, current, index
}

// findEmpty 返回桶里第一个空槽，没有则返回 -1。
func (f *Filter) findEmpty(index int) int {
	for slot := 0; slot < BucketSize; slot++ {
		if f.buckets[index][slot] == 0 {
			return slot
		}
	}
	return -1
}

// grow 把桶数翻倍并重建：按新桶数重算每个存活元素的两个候选桶后重新插入。
// 仅在踢出失败时调用，不会每次插入都复制整表。
func (f *Filter) grow() error {
	oldItems := f.items
	target := len(f.buckets) * 2
	for attempt := 0; attempt < maxGrowths; attempt++ {
		f.buckets = make([][BucketSize]uint16, target)
		f.count = 0
		ok := true
		for _, item := range oldItems {
			if placed, evicted, _ := f.place(f.fingerprint(item), f.index1(item)); !placed {
				// 被踢出的指纹丢弃引用即可：它仍属于某个存活元素，
				// 本轮重建作废，换更大的桶数从头再来。
				_ = evicted
				ok = false
				break
			}
		}
		if ok {
			return nil
		}
		target <<= 1
	}
	return ErrFull
}

// Lookup 查询：两个候选桶里任一槽指纹相同即命中。
func (f *Filter) Lookup(item string) bool {
	fp := f.fingerprint(item)
	i1 := f.index1(item)
	if f.contains(fp, i1) {
		return true
	}
	return f.contains(fp, f.altIndex(i1, fp))
}

func (f *Filter) contains(fp uint16, index int) bool {
	for slot := 0; slot < BucketSize; slot++ {
		if f.buckets[index][slot] == fp {
			return true
		}
	}
	return false
}

// Delete 只清指纹与候选桶都匹配的槽；删不存在的元素返回 ErrMissing，不误删他人。
func (f *Filter) Delete(item string) error {
	fp := f.fingerprint(item)
	i1 := f.index1(item)
	if slot := f.findFingerprint(fp, i1); slot >= 0 {
		f.buckets[i1][slot] = 0
		f.count--
		f.forgetItem(item)
		return nil
	}
	i2 := f.altIndex(i1, fp)
	if slot := f.findFingerprint(fp, i2); slot >= 0 {
		f.buckets[i2][slot] = 0
		f.count--
		f.forgetItem(item)
		return nil
	}
	return ErrMissing
}

// forgetItem 从存活元素列表移除一个匹配项（交换到末尾后截断，顺序无关但确定）。
func (f *Filter) forgetItem(item string) {
	for i := len(f.items) - 1; i >= 0; i-- {
		if f.items[i] == item {
			last := len(f.items) - 1
			f.items[i] = f.items[last]
			f.items = f.items[:last]
			return
		}
	}
}

func (f *Filter) findFingerprint(fp uint16, index int) int {
	for slot := 0; slot < BucketSize; slot++ {
		if f.buckets[index][slot] == fp {
			return slot
		}
	}
	return -1
}

// Count 是存活元素数。
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
