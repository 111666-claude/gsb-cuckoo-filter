# cuckoo-filter

布谷鸟过滤器（两个候选桶 + 指纹踢出），只用 Go 标准库。

```
go test ./...
go run ./cmd/cuckoo-filter --capacity 1024 --insert 5000 --lookup
go run ./cmd/cuckoo-filter --capacity 16 --insert 100 --lookup
```

## 口径（README 为准）

- **两个候选桶必须不同**：`index2 = index1 ^ (指纹的另一个哈希 % 桶数)`，不能与 `index1` 相同。
  `--capacity 1024 --insert 5000 --lookup` 现在两个桶是同一个位置，插入时互相踢，
  最后被踢出的指纹直接丢掉，`false_negative` 远大于 0；按口径必须为 0。
- **踢出要真的换桶，装不下要扩容**：踢出的指纹要用它的另一个候选桶继续找位置，最多 `MaxKicks` 次；
  仍然失败时必须扩容重建（容量翻倍）后重试插入，不能 `return nil` 假装成功。
  `--capacity 16 --insert 100 --lookup` 按口径要 `false_negative=0` 且 `load ≤ 0.9`。
- **删除要精确**：只删指纹和候选桶都匹配的那个槽，删不存在的元素要报 `ErrMissing`。
- 不变量：插入成功的元素后来必须查得到（零假阴性）；误报率要低（1 万个随机元素 ≤ 5%）；
  `Count` 等于当前存活元素数；同一输入重复执行结果相同。
- 规模：10 万次插入 + 10 万次查询要在 1 秒内完成，内存 O(容量)；扩容只重建一次，不许每次插入都复制整表。

## 实现要点

- `alternate(index, fp) = index ^ (fp 的 FNV-1a % 桶数)`，偏移为 0 时取 1，
  保证两个候选桶不同且运算对称（被踢指纹沿另一个候选桶继续找位置）。
- 两个候选桶都满时随机踢出一个槽位的指纹，在其另一个候选桶继续找空位，最多 `MaxKicks`（500）次。
- 500 次仍失败则桶数翻倍，按存活元素重建整张表后重试插入；连续翻倍仍失败才返回 `ErrFull`。
- 重建只在踢出失败时发生，不是每次插入都复制整表；存活元素表与指纹表均为 O(容量)。
- `Delete` 只清指纹与候选桶都匹配的槽，元素不在过滤器中返回 `ErrMissing`。
- 随机踢出使用固定种子，同一输入重复执行结果完全一致。

## 计划新增的能力

```
--capacity N   初始容量
--insert N     插入 key-0..key-N-1
--delete N     删除前 N 个 key
--lookup       统计假阴性与误报
```

## 输出

```
inserted=5000 count=5000 capacity=8192 load=0.610
false_negative=0 false_positive=3
```

## 目录

```
cuckoo.go              过滤器
cmd/cuckoo-filter      命令行入口
cuckoo_test.go         go test 用例
```
