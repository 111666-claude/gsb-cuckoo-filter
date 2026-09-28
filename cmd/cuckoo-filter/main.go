// 命令行入口：构造布谷鸟过滤器并统计假阴性、误报。
package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"

	cuckoo "example.com/cuckoo"
)

func main() {
	capacity := flag.Int("capacity", 1024, "初始容量（槽数，内部向上取整到 4 的倍数且桶数为 2 的幂）")
	inserts := flag.Int("insert", 0, "插入 key-0..key-N-1")
	deletes := flag.Int("delete", 0, "删除 key-0..key-N-1")
	lookup := flag.Bool("lookup", false, "统计假阴性与误报")
	fpBits := flag.Int("fingerprint-bits", 16, "指纹位数（2..16）")
	flag.Parse()

	filter := cuckoo.New(*capacity, *fpBits)

	for i := 0; i < *inserts; i++ {
		if err := filter.Insert("key-" + strconv.Itoa(i)); err != nil {
			fmt.Fprintf(os.Stderr, "插入 key-%d 失败：%v\n", i, err)
			os.Exit(1)
		}
	}
	for i := 0; i < *deletes; i++ {
		if err := filter.Delete("key-" + strconv.Itoa(i)); err != nil {
			fmt.Fprintf(os.Stderr, "删除 key-%d 失败：%v\n", i, err)
			os.Exit(1)
		}
	}

	fmt.Printf("inserted=%d count=%d capacity=%d load=%.3f\n",
		*inserts, filter.Count(), filter.Capacity(), filter.Load())

	if *lookup {
		falseNegative := 0
		for i := 0; i < *inserts; i++ {
			if i < *deletes {
				continue
			}
			if !filter.Lookup("key-" + strconv.Itoa(i)) {
				falseNegative++
			}
		}
		falsePositive := 0
		const queries = 10000
		for i := 0; i < queries; i++ {
			if filter.Lookup("absent-" + strconv.Itoa(i)) {
				falsePositive++
			}
		}
		fmt.Printf("false_negative=%d false_positive=%d\n", falseNegative, falsePositive)
	}
}
