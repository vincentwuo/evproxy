package lb

import (
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/cespare/xxhash"
)

func TestIPhashXXHash(t *testing.T) {
	addrs := []string{"1.1.1.1", "1.1.1.2", "1.1.1.50", "1.1.1.100", "222.186.133.1"}
	for _, addr := range addrs {
		fmt.Println(xxhash.Sum64String(addr) % 3)
	}
}

func TestIPhashFunction(t *testing.T) {
	lb, err := NewIPhash("tcp", []string{"1.1.1.1:1", "2.1.1.2:1", "3.1.1.50:1", "4.1.1.100:1", "5.1.1.1:1"}, 10, 100*time.Minute, 100*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	var sourceAddr []string
	for i := 0; i < 25; i++ {
		sourceAddr = append(sourceAddr, "123.123.123."+strconv.Itoa(i))
	}
	var distributionStatistic = make(map[string]int)
	for _, addr := range sourceAddr {
		destinationAddr, err := lb.Get(addr)
		distributionStatistic[destinationAddr.AddrStr] += 1
		if err != nil {
			t.Fatal(err)
		}
	}
	fmt.Println("old:", distributionStatistic)

	lb.Unreachable("1.1.1.1:1")
	distributionStatistic = make(map[string]int)
	for _, addr := range sourceAddr {
		destinationAddr, err := lb.Get(addr)
		distributionStatistic[destinationAddr.AddrStr] += 1
		if err != nil {
			t.Fatal(err)
		}
	}
	fmt.Println("new:", distributionStatistic)
}

func TestIPhashDistribution(t *testing.T) {
	lb, err := NewIPhash("tcp", []string{"1.1.1.1:1", "2.1.1.2:1", "3.1.1.50:1", "4.1.1.100:1", "5.1.1.1:1"}, 10, 100*time.Minute, 100*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	var sourceAddr []string
	for i := 0; i < 256; i++ {
		sourceAddr = append(sourceAddr, "123.123.123."+strconv.Itoa(i))
	}
	for i := 0; i < 256; i++ {
		sourceAddr = append(sourceAddr, "222.186.137."+strconv.Itoa(i))
	}
	for i := 0; i < 256; i++ {
		sourceAddr = append(sourceAddr, "102.1.2."+strconv.Itoa(i))
	}
	var distributionStatistic = make(map[string]int)
	for _, addr := range sourceAddr {
		destinationAddr, err := lb.Get(addr)
		distributionStatistic[destinationAddr.AddrStr] += 1
		if err != nil {
			t.Fatal(err)
		}
	}
	fmt.Println(distributionStatistic)

}
