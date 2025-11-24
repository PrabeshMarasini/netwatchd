package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"netwatchd/pdh"
)

type MonitoringData struct {
	mu 					sync.Mutex
	packetBuckets		[]int 
	bandwidthBuckets	[]float64 
	currentPackets		int
	currentBandwidth	float64
	startTime			time.Time 
	nextBucketTime		time.Time
}

func main() {
	interfaceFlag := flag.String("i", "", "Interface to capture on (leave empty to list all)")
	durationFlag := flag.Int("d", 10, "Capture duration in seconds")
	filterFlag := flag.String("f", "", "BPF filter (e.g., 'tcp port 80')")
	enableBandwidth := flag.Bool("b", true, "Enable bandwidth monitoring (Windows only)")
	adapterFlag := flag.String("a", "", "Network adapter for bandwidth monitoring (leave empty for auto-select)")
	flag.Parse()

	if *interfaceFlag == "" {
		listInterfaces()
		return
	}

	// Initialize data monitoring
	data := &MonitoringData{
		startTime:		time.Now(),
		nextBucketTime: time.Now().Add(1 * time.Minute),
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*durationFlag)*time.Second)
	defer cancel()

	var wg sync.WaitGroup

	//Start packet capture
	wg.Add(1)
	go func() {
		defer wg.Done()
		capturePackets(ctx, data, *interfaceFlag, *filterFlag)
	}()

	// Start bandwidth monitoring for windows
	if *enableBandwidth && runtime.GOOS == "windows" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			monitorBandwidth(ctx, data, *adapterFlag)
		}()
	}

	// Bucket management goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		manageBuckets(ctx, data)
	}()

	wg.Wait()
	generateReport(data)
}

func listInterfaces() {
	cmd := exec.Command("tshark", "-D")
	output, err := cmd.Output()
	if err != nil {
		fmt.Printf("Error listing interfaces: %v\n", err)
		fmt.Println("Make sure tshark is installed and in your PATH")
		return
	}

	fmt.Println("Available network interfaces:")
	fmt.Println(string(output))
	fmt.Println("\nUsage: go run main.go -i <interface_number> -d <seconds> -f '<filter>' -b -a '<adapter>'")
	fmt.Println("Example: go run main.go -i 1 -d 30 -f 'tcp port 443' -b")
}

func manageBuckets(ctx context.Context, data *MonitoringData) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			data.mu.Lock()
			if now.After(data.nextBucketTime) {
				// Move to next bucket
				data.packetBuckets = append(data.packetBuckets, data.currentPackets)
				data.bandwidthBuckets = append(data.bandwidthBuckets, data.currentBandwidth)
				data.currentPackets = 0
				data.currentBandwidth = 0
				data.nextBucketTime = data.nextBucketTime.Add(1 * time.Minute)
			}
			data.mu.Unlock()
		}
	}
}

func capturePackets(ctx context.Context, data *MonitoringData, iface, filter string) {
	args := []string{
		"-i", iface,
		"-l",
	}

	if filter != "" {
		args = append(args, "-f", filter)
	}

	fmt.Printf("Starting packet capture on interface %s...\n", iface)
	if filter != "" {
		fmt.Printf("Filter: %s\n", filter)
	}
	fmt.Println("---")

	cmd := exec.CommandContext(ctx, "tshark", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		fmt.Printf("Error setting up pipe: %v\n", err)
		return
	}

	if err := cmd.Start(); err != nil {
		fmt.Printf("Error starting tshark: %v\n", err)
		return
	}

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		select {
		case <-ctx.Done():
			return
		default:
			fmt.Println(line) // Show packet in real-time
			data.mu.Lock()
			data.currentPackets++
			data.mu.Unlock()
		}
	}
	cmd.Wait()
}

func monitorBandwidth(ctx context.Context, data *MonitoringData, adapterName string) {
	if err := pdh.Initialize(); err != nil {
		fmt.Printf("Failed to initialize PDH: %v\n", err)
		return
	}
	defer pdh.Cleanup()

	// Get adapter if not specified
	if adapterName == "" {
		adapters, err := pdh.GetNetworkAdapters()
		if err != nil || len(adapters) == 0 {
			fmt.Printf("Failed to get network adapters: %v\n", err)
			return
		}
		adapterName = adapters[0]
	}

	// Bandwidth monitoring running silently in background

	sentCounter, err := pdh.NewCounter(adapterName, "Bytes Sent/sec")
	if err != nil {
		fmt.Printf("Failed to create sent counter: %v\n", err)
		return
	}
	defer sentCounter.Close()

	recvCounter, err := pdh.NewCounter(adapterName, "Bytes Received/sec")
	if err != nil {
		fmt.Printf("failed to create received counter: %v\n", err)
		return
	}
	defer recvCounter.Close()

	// Initial collection
	pdh.CollectData()
	time.Sleep(1 * time.Second)

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := pdh.CollectData(); err != nil {
				continue
			}

			sentBytes, err1 := sentCounter.GetValue()
			recvBytes, err2 := recvCounter.GetValue()

			if err1 == nil && err2 == nil {
				totalBytes := sentBytes + recvBytes
				data.mu.Lock()
				data.currentBandwidth += totalBytes
				data.mu.Unlock()
			}
		}
	}
}

func generateReport(data *MonitoringData) {
	data.mu.Lock()
	defer data.mu.Unlock()
	data.packetBuckets = append(data.packetBuckets, data.currentPackets)
	data.bandwidthBuckets = append(data.bandwidthBuckets, data.currentBandwidth)

	elapsed := time.Since(data.startTime)
	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("MONITORING REPORT")
	fmt.Println(strings.Repeat("=", 60))

	totalPackets := 0
	totalBandwidth := 0.0

	for i := 0; i < len(data.packetBuckets); i++ {
		packets := data.packetBuckets[i]
		var bandwidth float64
		if i < len(data.bandwidthBuckets) {
			bandwidth = data.bandwidthBuckets[i]
		}

		totalPackets += packets
		totalBandwidth += bandwidth

		if i == len(data.packetBuckets)-1 {
			remainingSeconds := int(elapsed.Seconds()) - i*60
			if remainingSeconds < 60 {
				bandwidthMB := bandwidth / (1024 * 1024) 
				fmt.Printf("last %d seconds: %d packets | %.2f MB\n", remainingSeconds, packets, bandwidthMB)
				break
			}
		}

		bandwidthMB := bandwidth / (1024 * 1024)
		fmt.Printf("minute %d: %d packets | %.2f MB\n", i+1, packets, bandwidthMB)
	}

	fmt.Println(strings.Repeat("-", 60))
	totalBandwidthMB := totalBandwidth / (1024 * 1024)
	fmt.Printf("TOTAL: %d packets | %.2f MB\n", totalPackets, totalBandwidthMB)

	if totalPackets > 0 {
		avgBytesPerPacket := totalBandwidth / float64(totalPackets)
		fmt.Printf("Average bytes per packet: %.2f\n", avgBytesPerPacket)
	}

	fmt.Println(strings.Repeat("=", 60))
}