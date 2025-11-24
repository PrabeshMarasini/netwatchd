package linux

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type NetstatMonitor struct {
	interfaces map[string]*InterfaceStats
}

type InterfaceStats struct {
	Name     string
	RxBytes  uint64
	TxBytes  uint64
	LastRx   uint64
	LastTx   uint64
}

type Counter struct {
	interfaceName string
	counterType   string // "rx" or "tx"
	monitor       *NetstatMonitor
}

func Initialize() error {
	// Check if /proc/net/dev exists
	if _, err := os.Stat("/proc/net/dev"); os.IsNotExist(err) {
		return fmt.Errorf("/proc/net/dev not found - Linux network stats unavailable")
	}
	return nil
}

func Cleanup() {
}

func GetNetworkAdapters() ([]string, error) {
	file, err := os.Open("/proc/net/dev")
	if err != nil {
		return nil, fmt.Errorf("failed to open /proc/net/dev: %v", err)
	}
	defer file.Close()

	var adapters []string
	scanner := bufio.NewScanner(file)
	
	// Skip first two header lines
	scanner.Scan()
	scanner.Scan()

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		
		parts := strings.Fields(line)
		if len(parts) > 0 {
			interfaceName := strings.TrimSuffix(parts[0], ":")
			if interfaceName != "lo" {
				adapters = append(adapters, interfaceName)
			}
		}
	}

	return adapters, scanner.Err()
}

func NewMonitor() *NetstatMonitor {
	return &NetstatMonitor{
		interfaces: make(map[string]*InterfaceStats),
	}
}

func NewCounter(adapterName, counterType string) (*Counter, error) {
	monitor := NewMonitor()
	
	// Validate adapter exists
	adapters, err := GetNetworkAdapters()
	if err != nil {
		return nil, err
	}
	
	found := false
	for _, adapter := range adapters {
		if adapter == adapterName {
			found = true
			break
		}
	}
	
	if !found {
		return nil, fmt.Errorf("network adapter '%s' not found", adapterName)
	}

	var cType string
	switch counterType {
	case "Bytes Sent/sec":
		cType = "tx"
	case "Bytes Received/sec":
		cType = "rx"
	default:
		return nil, fmt.Errorf("unsupported counter type: %s", counterType)
	}

	return &Counter{
		interfaceName: adapterName,
		counterType:   cType,
		monitor:       monitor,
	}, nil
}

func CollectData() error {
	return nil
}

func (c *Counter) GetValue() (float64, error) {
	file, err := os.Open("/proc/net/dev")
	if err != nil {
		return 0, fmt.Errorf("failed to open /proc/net/dev: %v", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	
	// Skip header lines
	scanner.Scan()
	scanner.Scan()

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) < 17 {
			continue
		}

		interfaceName := strings.TrimSuffix(parts[0], ":")
		if interfaceName != c.interfaceName {
			continue
		}

		// Parse RX and TX bytes (col 1 & 9)
		rxBytes, err1 := strconv.ParseUint(parts[1], 10, 64)
		txBytes, err2 := strconv.ParseUint(parts[9], 10, 64)

		if err1 != nil || err2 != nil {
			return 0, fmt.Errorf("failed to parse network stats for %s", interfaceName)
		}

		// Get or create interface stats
		stats, exists := c.monitor.interfaces[interfaceName]
		if !exists {
			stats = &InterfaceStats{
				Name:   interfaceName,
				LastRx: rxBytes,
				LastTx: txBytes,
			}
			c.monitor.interfaces[interfaceName] = stats
			return 0, nil
		}

		// Calculate bytes per second since last reading
		var bytesPerSec float64
		if c.counterType == "rx" {
			bytesPerSec = float64(rxBytes - stats.LastRx)
			stats.LastRx = rxBytes
		} else {
			bytesPerSec = float64(txBytes - stats.LastTx)
			stats.LastTx = txBytes
		}

		stats.RxBytes = rxBytes
		stats.TxBytes = txBytes

		return bytesPerSec, nil
	}

	return 0, fmt.Errorf("interface %s not found in /proc/net/dev", c.interfaceName)
}

func (c *Counter) Close() {
}
