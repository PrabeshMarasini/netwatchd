package pdh

import (
	"fmt"
	"syscall"
	"unsafe"
)

var (
	pdh                    = syscall.NewLazyDLL("pdh.dll")
	pdhOpenQuery           = pdh.NewProc("PdhOpenQueryW")
	pdhAddEnglishCounterW  = pdh.NewProc("PdhAddEnglishCounterW")
	pdhAddCounterW         = pdh.NewProc("PdhAddCounterW")
	pdhCollectQueryData    = pdh.NewProc("PdhCollectQueryData")
	pdhGetFormattedCounter = pdh.NewProc("PdhGetFormattedCounterValue")
	pdhCloseQuery          = pdh.NewProc("PdhCloseQuery")
	pdhExpandWildCardPathW = pdh.NewProc("PdhExpandWildCardPathW")
)

const (
	PDH_FMT_DOUBLE   = 0x00000200
	PDH_INVALID_DATA = 0xC0000BBA
	PDH_NO_DATA      = 0x800007D5
)

var query uintptr

type Counter struct {
	handle uintptr
}

type PDH_FMT_COUNTERVALUE struct {
	CStatus     uint32
	DoubleValue float64
}

// Creating a PDH query
func Initialize() error {
	var q uintptr
	ret, _, _ := pdhOpenQuery.Call(0, 0, uintptr(unsafe.Pointer(&q)))
	if ret != 0 {
		return fmt.Errorf("PdhOpenQuery failed with code 0x%X", ret)
	}
	query = q
	return nil
}

// Closing the PDH query
func Cleanup() {
	if query != 0 {
		pdhCloseQuery.Call(query)
		query = 0
	}
}

// Listing all available network adapter names
func GetNetworkAdapters() ([]string, error) {
	wildcardPath := "\\Network Interface(*)\\Bytes Received/sec"
	pathPtr, _ := syscall.UTF16PtrFromString(wildcardPath)
	
	var bufSize uint32
	
	ret, _, _ := pdhExpandWildCardPathW.Call(
		0,
		uintptr(unsafe.Pointer(pathPtr)),
		0,
		uintptr(unsafe.Pointer(&bufSize)),
		0,
	)
	
	if ret != 0 && bufSize == 0 {
		return nil, fmt.Errorf("PdhExpandWildCardPathW failed: 0x%X", ret)
	}
	
	buf := make([]uint16, bufSize)
	ret, _, _ = pdhExpandWildCardPathW.Call(
		0,
		uintptr(unsafe.Pointer(pathPtr)),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&bufSize)),
		0,
	)
	
	if ret != 0 {
		return nil, fmt.Errorf("PdhExpandWildCardPathW second call failed: 0x%X", ret)
	}
	
	paths := parseMultiString(buf)
	var adapters []string
	
	for _, path := range paths {
		start := -1
		end := -1
		for i, ch := range path {
			if ch == '(' {
				start = i + 1
			} else if ch == ')' && start != -1 {
				end = i
				break
			}
		}
		if start != -1 && end != -1 && end > start {
			adapters = append(adapters, path[start:end])
		}
	}
	
	return adapters, nil
}

// Creating a performance counter for a specific network adapter
func NewCounter(adapterName, counterName string) (*Counter, error) {
	if query == 0 {
		return nil, fmt.Errorf("PDH not initialized")
	}

	path := fmt.Sprintf("\\Network Interface(%s)\\%s", adapterName, counterName)
	pathPtr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}

	var counterHandle uintptr
	
	ret, _, _ := pdhAddEnglishCounterW.Call(
		query,
		uintptr(unsafe.Pointer(pathPtr)),
		0,
		uintptr(unsafe.Pointer(&counterHandle)),
	)
	
	if ret != 0 {
		ret, _, _ = pdhAddCounterW.Call(
			query,
			uintptr(unsafe.Pointer(pathPtr)),
			0,
			uintptr(unsafe.Pointer(&counterHandle)),
		)
		if ret != 0 {
			return nil, fmt.Errorf("PdhAddCounterW failed for '%s' with code 0x%X", path, ret)
		}
	}

	return &Counter{handle: counterHandle}, nil
}

func CollectData() error {
	if query == 0 {
		return fmt.Errorf("PDH not initialized")
	}

	ret, _, _ := pdhCollectQueryData.Call(query)
	if ret != 0 && ret != PDH_INVALID_DATA {
		return fmt.Errorf("PdhCollectQueryData failed with code 0x%X", ret)
	}
	return nil
}

// Retrieving the current counter value
func (c *Counter) GetValue() (float64, error) {
	var value PDH_FMT_COUNTERVALUE
	ret, _, _ := pdhGetFormattedCounter.Call(
		c.handle,
		PDH_FMT_DOUBLE,
		0,
		uintptr(unsafe.Pointer(&value)),
	)
	if ret != 0 && ret != PDH_INVALID_DATA {
		return 0, fmt.Errorf("PdhGetFormattedCounterValue failed with code 0x%X", ret)
	}
	return value.DoubleValue, nil
}

// Counter cleanup
func (c *Counter) Close() {
}

func parseMultiString(buf []uint16) []string {
	var result []string
	var current []uint16

	for i := 0; i < len(buf); i++ {
		if buf[i] == 0 {
			if len(current) > 0 {
				result = append(result, syscall.UTF16ToString(current))
				current = nil
			}
			if i+1 < len(buf) && buf[i+1] == 0 {
				break
			}
		} else {
			current = append(current, buf[i])
		}
	}

	return result
}