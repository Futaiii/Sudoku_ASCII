package geoip

import (
	"bufio"
	"encoding/binary"
	"log"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// IPRange 表示一个 IP 区间 [Start, End]
type IPRange struct {
	Start uint32
	End   uint32
}

type Manager struct {
	ranges []IPRange
	mu     sync.RWMutex
	url    string
}

var instance *Manager
var once sync.Once

// GetInstance 单例模式
func GetInstance(url string) *Manager {
	once.Do(func() {
		instance = &Manager{
			url: url,
		}
		// 异步初始化，避免阻塞启动
		go instance.Update()
	})
	return instance
}

// Update 下载并更新规则
func (m *Manager) Update() {
	log.Printf("[GeoIP] Downloading rules from %s...", m.url)

	client := http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(m.url)
	if err != nil {
		log.Printf("[GeoIP] Update failed: %v", err)
		return
	}
	defer resp.Body.Close()

	var tempRanges []IPRange
	scanner := bufio.NewScanner(resp.Body)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		_, ipNet, err := net.ParseCIDR(line)
		if err != nil {
			// 尝试作为普通 IP 解析
			ip := net.ParseIP(line)
			if ip != nil {
				val := ipToUint32(ip)
				tempRanges = append(tempRanges, IPRange{Start: val, End: val})
			}
			continue
		}

		start := ipToUint32(ipNet.IP)
		// 计算结束 IP: start | ^mask
		mask := binary.BigEndian.Uint32(ipNet.Mask)
		end := start | (^mask)

		tempRanges = append(tempRanges, IPRange{Start: start, End: end})
	}

	// 核心优化：区间合并与排序 (构建线段树/区间树的静态形式)
	merged := mergeRanges(tempRanges)

	m.mu.Lock()
	m.ranges = merged
	m.mu.Unlock()

	log.Printf("[GeoIP] Loaded %d CN CIDR rules (Merged into %d ranges)", len(tempRanges), len(merged))
}

// Contains 检查 IP 是否在列表中 (二分查找 O(log N))
func (m *Manager) Contains(ip net.IP) bool {
	ip4 := ip.To4()
	if ip4 == nil {
		return false // 暂不支持 IPv6 路由，默认 false (走代理)
	}
	val := ipToUint32(ip4)

	m.mu.RLock()
	defer m.mu.RUnlock()

	// 在排序的区间中查找
	// 找到第一个 range.End >= val 的索引
	idx := sort.Search(len(m.ranges), func(i int) bool {
		return m.ranges[i].End >= val
	})

	if idx < len(m.ranges) && m.ranges[idx].Start <= val {
		return true
	}
	return false
}

func ipToUint32(ip net.IP) uint32 {
	if len(ip) == 16 {
		return binary.BigEndian.Uint32(ip[12:16])
	}
	return binary.BigEndian.Uint32(ip)
}

// mergeRanges 对区间进行排序并合并重叠部分
func mergeRanges(ranges []IPRange) []IPRange {
	if len(ranges) == 0 {
		return nil
	}

	// 1. 按 Start 排序
	sort.Slice(ranges, func(i, j int) bool {
		return ranges[i].Start < ranges[j].Start
	})

	var result []IPRange
	current := ranges[0]

	for i := 1; i < len(ranges); i++ {
		next := ranges[i]
		// 如果当前区间包含或连接下一个区间
		if current.End >= next.Start-1 { // -1 处理连续区间，如 1.2.3.4 和 1.2.3.5
			if next.End > current.End {
				current.End = next.End
			}
		} else {
			result = append(result, current)
			current = next
		}
	}
	result = append(result, current)
	return result
}
