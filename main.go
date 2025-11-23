package main

import (
	"bufio"
	"flag"
	"fmt"
	"os/exec"
	"time"
)

func main() {
	interfaceFlag := flag.String("i", "", "Interface to capture on (leave empty to list all)")
	durationFlag := flag.Int("d", 10, "Capture duration in seconds")
	filterFlag := flag.String("f", "", "BPF filter (e.g., 'tcp port 80')")
	flag.Parse()

	if *interfaceFlag == "" {
		listInterfaces()
		return
	}

	capturePackets(*interfaceFlag, *durationFlag, *filterFlag)
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
	fmt.Println("\nUsage: go run main.go -i <interface_number> -d <seconds> -f '<filter>'")
	fmt.Println("Example: go run main.go -i 1 -d 30 -f 'tcp port 443'")
}

func capturePackets(iface string, duration int, filter string) {
	args := []string{
		"-i", iface,
		"-a", fmt.Sprintf("duration:%d", duration),
		"-l",
	}

	startTime := time.Now()
	bucketCounts := make([]int, 0)
	currentBucket := 0
	nextBucketTime := startTime.Add(1 * time.Minute)

	if filter != "" {
		args = append(args, "-f", filter)
	}

	fmt.Printf("Starting capture on interface %s for %d seconds...\n", iface, duration)
	if filter != "" {
		fmt.Printf("Filter: %s\n", filter)
	}
	fmt.Println("---")

	cmd := exec.Command("tshark", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		fmt.Printf("Error setting up pipe: %v\n", err)
		return
	}

	if err := cmd.Start(); err != nil {
		fmt.Printf("Error starting tshark: %v\n", err)
		fmt.Println("Make sure tshark is installed and running as Administrator")
		return
	}

	scanner := bufio.NewScanner(stdout)
	lineCount := 0
	for scanner.Scan() {
		line := scanner.Text()

		now := time.Now()

		for now.After(nextBucketTime) {
			bucketCounts = append(bucketCounts, currentBucket)
			currentBucket = 0
			nextBucketTime = nextBucketTime.Add(1 * time.Minute)
		}
		currentBucket++
		fmt.Println(line)
		lineCount++
	}

	if err := cmd.Wait(); err != nil {
		fmt.Printf("tshark error: %v\n", err)
	}

	bucketCounts = append(bucketCounts, currentBucket)
	elapsed := time.Since(startTime)

	for i, count := range bucketCounts {
    if i == len(bucketCounts)-1 {
        remainingSeconds := int(elapsed.Seconds()) - i*60
        if remainingSeconds < 60 {
            fmt.Printf("Last %d seconds: %d protocols\n", remainingSeconds, count)
			break
        }
    }
    fmt.Printf("Minute %d: %d protocols\n", i+1, count)
}

	fmt.Println("---")
	fmt.Printf("Capture complete. Total lines: %d\n", lineCount)
}