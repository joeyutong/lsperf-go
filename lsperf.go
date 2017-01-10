package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

var (
	lsPath = "/home/ty/elk/logstash-5.1.1/"
	//lsPath     = "/home/ty/elk/logstash-2.4.0/"
	configPath = "config/simple.conf"
	log4jPath  = "config/log4j.conf"
	inputPath  = "input/simple.txt"
	lastMsg    = ">>> lorem ipsum stop"
	statsURL   = "http://192.168.1.116:9600/_node/stats/process"
)

// Stats represents test output statistics
type Stats struct {
	Workers        int
	StartupElapsed time.Duration
	EventsElasped  time.Duration
	CPUUsage       int
}

type processStats struct {
	Host    string  `json:"host"`
	Process process `json:"process"`
}

type process struct {
	Mem mem `json:"mem"`
	CPU cpu `json:"cpu"`
}

type mem struct {
	VirtualInBytes float32 `json:"total_virtual_in_bytes"`
}

type cpu struct {
	Millis  int64   `json:"total_in_millis"`
	Percent int     `json:"percent"`
	LoadAVG loadAVG `json:"load_average:"`
}

type loadAVG struct {
	Load1m  float32 `json:"1m"`
	Load5m  float32 `json:"5m"`
	Load15m float32 `json:"15m"`
}

func run(workers int) {
	stats := Stats{}
	cmd := exec.Command(lsPath+"bin/logstash", "-w", strconv.Itoa(workers), "-f", log4jPath)
	stdout, _ := cmd.StdoutPipe()

	start := time.Now()
	if err := cmd.Start(); err != nil {
		log.Fatal(err)
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Split(bufio.ScanLines)
	for scanner.Scan() {
		s := scanner.Text()
		//fmt.Println("Logstash startup output:", s)
		if strings.Contains(s, "Successfully") {
			stats.Workers = workers
			stats.StartupElapsed = time.Since(start)
			break
		}
	}

	eventCount := 0
	eventNum := 4000000
	scanner = bufio.NewScanner(stdout)
	scanner.Split(bufio.ScanRunes)

	c := make(chan int)
	quit := make(chan int)
	go inputLog4jEvent(eventNum)
	go outputCPUStats(c, quit)

	start = time.Now()
	for scanner.Scan() {
		eventCount++
		//fmt.Println("Event output:", scanner.Text())
		if eventCount == eventNum {
			quit <- 0
			stats.EventsElasped = time.Since(start)
			stats.CPUUsage = <-c
			fmt.Println(stats)
			cmd.Process.Kill()
		}
	}
}

func inputStdinEvents(input io.Writer) {
	lines, _ := readLines(inputPath)
	loopeventCount := 100000 / len(lines)

	for i := 0; i < loopeventCount; i++ {
		for _, line := range lines {
			input.Write([]byte(line + "\n"))
		}
	}
	input.Write([]byte(lastMsg + "\n"))
}

func inputLog4jEvent(eventNum int) {
	cmd := exec.Command("java", "-jar", "input/repeat-log.jar", strconv.Itoa(eventNum))
	if err := cmd.Run(); err != nil {
		log.Fatal(err)
	}
}

func readLines(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, scanner.Err()
}

func outputCPUStats(c, quit chan int) {
	tick := time.Tick(1000 * time.Millisecond)
	eventCount := 0
	percentage := 0
	for {
		select {
		case <-tick:
			resp, err := http.Get(statsURL)
			if err != nil {
				log.Fatal(err)
			}

			stats := new(processStats)
			json.NewDecoder(resp.Body).Decode(stats)
			percentage += stats.Process.CPU.Percent
			eventCount++
			resp.Body.Close()
		case <-quit:
			c <- percentage / eventCount
			return
		}
	}
}

func main() {
	fmt.Println("[Pipeline workers]", "[Startup elasped]", "[Events elasped]", "[CPU %]")
	for i := 1; i < 8; i++ {
		run(i)
	}
}
