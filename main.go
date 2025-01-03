package main

import (
	"bufio"
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// DataPacket represents a dynamic form submission
type DataPacket map[string]string

// Config holds the configuration parsed from the data file
type Config struct {
	URL     string
	Method  string
	Headers []string
	Data    []DataPacket
}

func main() {
	// Define flags with default values
	fileFlag := flag.String("file", "./info.txt", "Path to the data file")
	workersFlag := flag.Int("workers", 5, "Number of concurrent workers")
	helpFlag := flag.Bool("help", false, "Display help information")

	// Customize Usage
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options]\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\nAutomate form submissions with dynamic fields.\n\n")
		flag.PrintDefaults()
	}

	flag.Parse()

	// Show help if -help is specified
	if *helpFlag {
		flag.Usage()
		os.Exit(0)
	}

	filePath := strings.TrimSpace(*fileFlag)
	workerCount := *workersFlag

	// Parse the data file
	config, err := parseDataFile(filePath)
	if err != nil {
		log.Fatalf("Error parsing data file: %v", err)
	}

	// Validate parsed configuration
	if config.URL == "" {
		log.Fatalf("URL not specified in the data file.")
	}

	if config.Method == "" {
		log.Fatalf("HTTP method not specified in the data file.")
	}

	if len(config.Headers) == 0 {
		log.Fatalf("No form headers found in the data file.")
	}

	if len(config.Data) == 0 {
		log.Fatalf("No data packets found in the data file.")
	}

	targetURL := config.URL
	method := strings.ToUpper(strings.TrimSpace(config.Method))

	// Initialize HTTP client with timeout
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// Channels and WaitGroup for concurrency
	jobs := make(chan DataPacket, workerCount*2)
	var wg sync.WaitGroup

	// Counters for tracking
	var successCount int64
	var failureCount int64

	// Start worker goroutines
	for w := 1; w <= workerCount; w++ {
		wg.Add(1)
		go worker(w, client, method, targetURL, jobs, &wg, &successCount, &failureCount)
	}

	// Send jobs to the workers
	for _, packet := range config.Data {
		jobs <- packet
	}
	close(jobs)

	// Wait for all workers to finish
	wg.Wait()

	// Calculate success percentage
	totalProcessed := atomic.LoadInt64(&successCount) + atomic.LoadInt64(&failureCount)
	percentage := 0.0
	if totalProcessed > 0 {
		percentage = (float64(successCount) / float64(totalProcessed)) * 100
	}

	// Summary
	fmt.Println("--- Form Flood v1.0 Summary ---")
	fmt.Printf("URL: %s\n", targetURL)
	fmt.Printf("HTTP Method: %s\n", method)
	fmt.Printf("Data File: %s\n", filePath)
	fmt.Println()
	fmt.Printf("Packets Parsed: %d\n", len(config.Data))
	fmt.Printf("Packets Processed: %d\n", totalProcessed)
	fmt.Printf("Successful Requests: %d\n", successCount)
	fmt.Printf("Failed Requests: %d\n", failureCount)
	fmt.Printf("Success Percentage: %.2f%%\n", percentage)
}

// parseDataFile reads the custom-formatted data file and parses it into a Config struct
func parseDataFile(filePath string) (*Config, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file '%s': %w", filePath, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	config := &Config{}
	currentSection := ""

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines
		if line == "" {
			continue
		}

		// Handle section headers
		if strings.HasPrefix(line, "#") {
			switch strings.ToLower(strings.TrimSpace(strings.TrimPrefix(line, "#"))) {
			case "required info":
				currentSection = "required_info"
			case "form names":
				currentSection = "form_names"
			case "form values":
				currentSection = "form_values"
			default:
				currentSection = ""
			}
			continue
		}

		// Parse lines based on the current section
		switch currentSection {
		case "required_info":
			parts := strings.SplitN(line, ":", 2)
			if len(parts) != 2 {
				log.Printf("Invalid line in Required Info section: '%s'", line)
				continue
			}
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			switch strings.ToLower(key) {
			case "url":
				config.URL = value
			case "method":
				config.Method = value
			default:
				log.Printf("Unknown key '%s' in Required Info section.", key)
			}
		case "form_names":
			headers, err := parseCSVLine(line)
			if err != nil {
				log.Printf("Error parsing Form Names: %v", err)
				continue
			}
			config.Headers = headers
		case "form_values":
			values, err := parseCSVLine(line)
			if err != nil {
				log.Printf("Error parsing Form Values: %v", err)
				continue
			}
			if len(values) != len(config.Headers) {
				log.Printf("Mismatch between number of headers and values: %v", values)
				continue
			}
			packet := make(DataPacket)
			for i, header := range config.Headers {
				packet[header] = values[i]
			}
			config.Data = append(config.Data, packet)
		default:
			// Ignore lines outside known sections
			log.Printf("Ignoring line outside of defined sections: '%s'", line)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading file '%s': %w", filePath, err)
	}

	return config, nil
}

// parseCSVLine parses a single CSV line into a slice of strings
func parseCSVLine(line string) ([]string, error) {
	reader := csv.NewReader(strings.NewReader(line))
	reader.TrimLeadingSpace = true
	return reader.Read()
}

// worker processes DataPackets from the jobs channel
func worker(id int, client *http.Client, method, targetURL string, jobs <-chan DataPacket, wg *sync.WaitGroup, successCount, failureCount *int64) {
	defer wg.Done()
	for packet := range jobs {
		resp, err := sendRequest(client, method, targetURL, packet)
		if err != nil {
			log.Printf("Worker %d: Request failed: %v\n", id, err)
			atomic.AddInt64(failureCount, 1)
			continue
		}

		// Determine success based on status code
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			atomic.AddInt64(successCount, 1)
		} else {
			atomic.AddInt64(failureCount, 1)
		}

		handleResponse(resp)
	}
}

// sendRequest constructs and sends an HTTP request based on the method and DataPacket
func sendRequest(client *http.Client, method, targetURL string, packet DataPacket) (*http.Response, error) {
	var req *http.Request
	var err error

	data := url.Values{}
	for key, value := range packet {
		data.Set(key, value)
	}

	if method == "GET" {
		u, err := url.Parse(targetURL)
		if err != nil {
			return nil, fmt.Errorf("invalid URL '%s': %w", targetURL, err)
		}
		u.RawQuery = data.Encode()
		req, err = http.NewRequest("GET", u.String(), nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create GET request: %w", err)
		}
	} else if method == "POST" {
		req, err = http.NewRequest("POST", targetURL, strings.NewReader(data.Encode()))
		if err != nil {
			return nil, fmt.Errorf("failed to create POST request: %w", err)
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		return nil, fmt.Errorf("unsupported HTTP method: %s", method)
	}

	req.Header.Set("User-Agent", "Form-Flooder/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	return resp, nil
}

// handleResponse processes and discards the HTTP response body
func handleResponse(resp *http.Response) {
	defer resp.Body.Close()

	_, err := io.Copy(io.Discard, resp.Body)
	if err != nil {
		log.Printf("Failed to read response body: %v", err)
	}
}
