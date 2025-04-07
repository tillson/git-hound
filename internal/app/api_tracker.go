package app

import (
	"sync"
	"time"

	"github.com/fatih/color"
)

var (
	apiRequestCount int
	apiMutex        sync.Mutex
)

// TrackAPIRequest logs and counts a GitHub API request.
// If the APIDebug flag is enabled, it will print details about the request.
func TrackAPIRequest(endpoint string, details string) {
	apiMutex.Lock()
	defer apiMutex.Unlock()

	apiRequestCount++

	if GetFlags().APIDebug {
		timestamp := time.Now().Format("15:04:05.000")
		color.Cyan("[API Request #%d @ %s] %s %s", apiRequestCount, timestamp, endpoint, details)
	}
}

// GetAPIRequestCount returns the current count of API requests.
func GetAPIRequestCount() int {
	apiMutex.Lock()
	defer apiMutex.Unlock()

	return apiRequestCount
}

// PrintAPIRequestSummary prints a summary of all API requests made.
func PrintAPIRequestSummary() {
	if GetFlags().APIDebug {
		color.Green("Total GitHub API Requests: %d", GetAPIRequestCount())
	}
}
