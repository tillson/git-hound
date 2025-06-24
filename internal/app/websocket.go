package app

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/gorilla/websocket"
)

var wsConn *websocket.Conn
var wsMessageChannel chan string
var WsAuthenticated chan bool
var isAuthenticated bool
var wsURL string // Store the WebSocket URL for reconnection

// var InsertKey string // Global variable for InsertKey

// StartWebSocket initializes the WebSocket connection with reconnection logic
func StartWebSocket(url string) {
	wsURL = url                          // Store URL for reconnection attempts
	WsAuthenticated = make(chan bool, 1) // Make channel buffered to prevent deadlock

	// Try to establish connection with reconnection logic
	success := establishWebSocketConnection(url)
	if !success {
		// If initial connection fails and we have a search_id, try reconnecting
		if GetFlags().SearchID != "" {
			color.Yellow("[!] Initial WebSocket connection failed. Attempting reconnection...")
			success = attemptWebSocketReconnection(url, 3)
			if !success {
				color.Red("[!] Failed to establish WebSocket connection after 3 attempts. Exiting.")
				os.Exit(1)
			}
			// Send authentication success signal
			WsAuthenticated <- true
		} else {
			color.Red("[!] WebSocket connection failed and no search_id provided. Exiting.")
			os.Exit(1)
		}
	}
}

// establishWebSocketConnection attempts to establish a single WebSocket connection
func establishWebSocketConnection(url string) bool {
	// color.Cyan("[*] Connecting to WebSocket at %s", url)
	dialer := websocket.Dialer{
		HandshakeTimeout:  5 * time.Second,
		EnableCompression: false,
	}
	var err error
	wsConn, _, err = dialer.Dial(url, nil)
	if err != nil {
		color.Red("Error connecting to GitHound Explore connector: %v", err)
		return false
	}
	color.Green("[+] WebSocket connection established")
	wsMessageChannel = make(chan string)

	// Start a goroutine to handle messages from the channel
	go func() {
		for {
			select {
			case msg := <-wsMessageChannel:
				if wsConn == nil {
					if GetFlags().Debug {
						color.Red("[DEBUG] WebSocket connection is nil, cannot send message")
					}
					continue
				}
				if GetFlags().Debug {
					color.Red("[DEBUG] Sending WebSocket message from channel: %s", msg)
				}
				err := wsConn.WriteMessage(websocket.TextMessage, []byte(msg))
				if err != nil {
					if GetFlags().Debug {
						color.Red("[DEBUG] Error sending message from channel: %v", err)
					}
					// Check if connection is lost and handle reconnection
					if !handleWebSocketDisconnection(err) {
						if GetFlags().Debug {
							color.Red("[DEBUG] Reconnection failed, breaking out of channel message loop")
						}
						return
					}
					// If reconnection was successful, try sending the message again
					if wsConn != nil {
						err = wsConn.WriteMessage(websocket.TextMessage, []byte(msg))
						if err != nil {
							if GetFlags().Debug {
								color.Red("[DEBUG] Error sending message after reconnection: %v", err)
							}
							// If it fails again, break out of the loop
							if !handleWebSocketDisconnection(err) {
								if GetFlags().Debug {
									color.Red("[DEBUG] Second reconnection attempt failed, breaking out of channel message loop")
								}
								return
							}
						}
					}
				}
			}
		}
	}()

	// Send initial message to start account linking
	var payload string
	if GetFlags().InsertKey != "" {
		payload = fmt.Sprintf(`{"event": "gh_banner", "ghVersion": "1.0.0", "insertToken": "%s"}`, GetFlags().InsertKey)
	} else {
		payload = fmt.Sprintf(`{"event": "gh_banner", "ghVersion": "1.0.0"}`)
	}
	fmt.Println(payload)
	if GetFlags().Debug {
		color.Red("[DEBUG] Sending WebSocket message: %s", payload)
	}
	err = wsConn.WriteMessage(websocket.TextMessage, []byte(payload))
	if err != nil {
		color.Red("Error sending WebSocket message: %v", err)
		WsAuthenticated <- false
		return false
	}

	// Handle initial response synchronously
	_, message, err := wsConn.ReadMessage()
	if err != nil {
		color.Red("Error reading initial WebSocket message: %v", err)
		WsAuthenticated <- false
		return false
	}

	var response map[string]interface{}
	err = json.Unmarshal(message, &response)
	if err != nil {
		color.Red("Error unmarshalling initial WebSocket message: %v", err)
		WsAuthenticated <- false
		return false
	}

	// If we have an insert token and the server confirms we're logged in, we're done
	if GetFlags().InsertKey != "" {
		if loggedIn, ok := response["logged_in"].(bool); ok && loggedIn {
			isAuthenticated = true
			WsAuthenticated <- true
			// If in trufflehog mode, we'll start the search from the main function
			if GetFlags().Trufflehog {
				// Search will be started from the main function
			}
		} else {
			color.Red("[!] Invalid insert token")
			isAuthenticated = false
			WsAuthenticated <- false
			return false
		}
	} else {
		// Handle account linking URL
		if url, ok := response["url"].(string); ok {
			color.Cyan("Please visit the following URL to link your account: %s", url)
			color.Cyan("Waiting for verification...")
		}

		// Start a goroutine to handle the authentication response
		go func() {
			for {
				_, message, err := wsConn.ReadMessage()
				if err != nil {
					color.Red("Error reading WebSocket message: %v", err)
					// Check if connection is lost and handle reconnection
					if !handleWebSocketDisconnection(err) {
						WsAuthenticated <- false
						return
					}
					continue
				}

				// Print the raw message for debugging
				if GetFlags().Debug {
					color.Cyan("[DEBUG] Received WebSocket message: %s", string(message))
				}

				var response map[string]interface{}
				if err := json.Unmarshal(message, &response); err != nil {
					color.Red("Error unmarshalling WebSocket message: %v", err)
					WsAuthenticated <- false
					return
				}

				// Print the parsed response for debugging
				if GetFlags().Debug {
					color.Cyan("[DEBUG] Parsed WebSocket response: %+v", response)
				}

				if loggedIn, ok := response["logged_in"].(bool); ok && loggedIn {
					if insertToken, ok := response["insert_token"].(string); ok {
						// Save the token
						homeDir, err := os.UserHomeDir()
						if err != nil {
							color.Red("Error getting home directory: %v", err)
							WsAuthenticated <- false
							return
						}

						gitHoundDir := filepath.Join(homeDir, ".githound")
						tokenFilePath := filepath.Join(gitHoundDir, "insert_token.txt")

						// Create the .githound directory if it doesn't exist
						if _, err := os.Stat(gitHoundDir); os.IsNotExist(err) {
							err = os.Mkdir(gitHoundDir, 0700)
							if err != nil {
								color.Red("Error creating .githound directory: %v", err)
								WsAuthenticated <- false
								return
							}
						}

						// Save the token to the file
						err = ioutil.WriteFile(tokenFilePath, []byte(insertToken), 0600)
						if err != nil {
							color.Red("Error writing token file: %v", err)
							WsAuthenticated <- false
							return
						}

						// Set the insert key and mark as authenticated
						GetFlags().InsertKey = insertToken
						isAuthenticated = true
						WsAuthenticated <- true
						return
					}
				}
			}
		}()

		// Don't send any value to WsAuthenticated - let the goroutine handle it
		isAuthenticated = false
	}

	return true
}

// attemptWebSocketReconnection attempts to reconnect to the WebSocket with retry logic
func attemptWebSocketReconnection(url string, maxAttempts int) bool {
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		color.Yellow("[*] WebSocket reconnection attempt %d/%d", attempt, maxAttempts)

		// Wait before retrying (exponential backoff)
		if attempt > 1 {
			waitTime := time.Duration(attempt) * 5 * time.Second
			color.Cyan("[*] Waiting %v before retry...", waitTime)
			time.Sleep(waitTime)
		}

		// Try to establish a basic connection first
		dialer := websocket.Dialer{
			HandshakeTimeout:  5 * time.Second,
			EnableCompression: false,
		}

		var err error
		wsConn, _, err = dialer.Dial(url, nil)
		if err != nil {
			color.Red("[!] WebSocket reconnection attempt %d failed: %v", attempt, err)
			continue
		}

		color.Green("[+] WebSocket connection re-established on attempt %d", attempt)

		// Re-authenticate if we have an insert key
		if GetFlags().InsertKey != "" {
			if reauthenticateAfterReconnection() {
				color.Green("[+] WebSocket reconnection and re-authentication successful")
				return true
			} else {
				color.Red("[!] WebSocket reconnection successful but re-authentication failed")
				wsConn.Close()
				wsConn = nil
				continue
			}
		} else {
			// If no insert key, just mark as authenticated (for cases where we don't need authentication)
			isAuthenticated = true
			color.Green("[+] WebSocket reconnection successful (no authentication required)")
			return true
		}
	}

	return false
}

// handleWebSocketDisconnection handles WebSocket disconnection and attempts reconnection if search_id is provided
func handleWebSocketDisconnection(err error) bool {
	// Check for various types of connection errors that should trigger reconnection
	shouldReconnect := false

	// Check for WebSocket close errors
	if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
		color.Red("WebSocket connection closed unexpectedly: %v", err)
		shouldReconnect = true
	}

	// Check for broken pipe and other TCP connection errors
	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "broken pipe") ||
			strings.Contains(errStr, "connection reset") ||
			strings.Contains(errStr, "connection refused") ||
			strings.Contains(errStr, "network is unreachable") ||
			strings.Contains(errStr, "no route to host") {
			color.Red("TCP connection error detected: %v", err)
			shouldReconnect = true
		}
	}

	if shouldReconnect {
		// If we have a search_id, try to reconnect
		if GetFlags().SearchID != "" {
			color.Yellow("[!] WebSocket connection lost. Attempting reconnection...")
			return attemptWebSocketReconnection(wsURL, 3)
		} else {
			color.Red("[!] WebSocket connection lost and no search_id provided. Exiting.")
			os.Exit(1)
		}
	}
	return false
}

// reauthenticateAfterReconnection handles re-authentication after a successful reconnection
func reauthenticateAfterReconnection() bool {
	if wsConn == nil {
		return false
	}

	// If we have an insert key, we can re-authenticate automatically
	if GetFlags().InsertKey != "" {
		payload := fmt.Sprintf(`{"event": "gh_banner", "ghVersion": "1.0.0", "insertToken": "%s"}`, GetFlags().InsertKey)
		if GetFlags().Debug {
			color.Red("[DEBUG] Sending re-authentication WebSocket message: %s", payload)
		}
		err := wsConn.WriteMessage(websocket.TextMessage, []byte(payload))
		if err != nil {
			color.Red("Error sending re-authentication message: %v", err)
			return false
		}

		// Read the response
		_, message, err := wsConn.ReadMessage()
		if err != nil {
			color.Red("Error reading re-authentication response: %v", err)
			return false
		}

		var response map[string]interface{}
		err = json.Unmarshal(message, &response)
		if err != nil {
			color.Red("Error unmarshalling re-authentication response: %v", err)
			return false
		}

		if loggedIn, ok := response["logged_in"].(bool); ok && loggedIn {
			isAuthenticated = true
			color.Green("[+] Re-authentication successful")
			return true
		} else {
			color.Red("[!] Re-authentication failed")
			return false
		}
	}

	// If no insert key, we can't re-authenticate automatically
	color.Yellow("[!] No insert key available for re-authentication")
	return false
}

func SendMessageToWebSocket(message string) {
	if wsMessageChannel != nil {
		wsMessageChannel <- message
	}
}

// SendToWebSocket sends a message to the WebSocket connection
func SendToWebSocket(message string) {
	if wsConn == nil {
		color.Yellow("[!] WebSocket not initialized")
		return
	}

	if GetFlags().Debug {
		color.Red("[DEBUG] Sending WebSocket message: %s", message)
	}
	err := wsConn.WriteMessage(websocket.TextMessage, []byte(message))
	if err != nil {
		color.Red("Error sending WebSocket message: %v", err)
		// Check if connection is lost and handle reconnection
		if handleWebSocketDisconnection(err) {
			// If reconnection was successful, try sending the message again
			err = wsConn.WriteMessage(websocket.TextMessage, []byte(message))
			if err != nil {
				color.Red("Error sending WebSocket message after reconnection: %v", err)
			}
		}
	}
}

func BrokerSearchCreation(query string) {
	if wsConn == nil {
		color.Red("WebSocket connection is not established")
		return
	}

	if !isAuthenticated {
		color.Red("WebSocket not authenticated")
		return
	}

	color.Cyan("[*] Starting search for query: %s", query)

	// Now send the search query
	escapedQuery, err := json.Marshal(query)
	if err != nil {
		color.Red("Error escaping search query")
		return
	}
	payload := fmt.Sprintf(`{"event": "start_search", "insertToken": "%s", "searchQuery": %s}`, GetFlags().InsertKey, escapedQuery)
	if GetFlags().Debug {
		color.Red("[DEBUG] Sending search WebSocket message: %s", payload)
	}
	err = wsConn.WriteMessage(websocket.TextMessage, []byte(payload))
	if err != nil {
		color.Red("Error sending search message: %v", err)
		// Check if connection is lost and handle reconnection
		if !handleWebSocketDisconnection(err) {
			return
		}
		// If reconnection was successful, try sending the message again
		err = wsConn.WriteMessage(websocket.TextMessage, []byte(payload))
		if err != nil {
			color.Red("Error sending search message after reconnection: %v", err)
			return
		}
	}

	_, message, err := wsConn.ReadMessage()
	if err != nil {
		color.Red("Error reading search response: %v", err)
		// Check if connection is lost and handle reconnection
		if !handleWebSocketDisconnection(err) {
			return
		}
		// If reconnection was successful, try reading the message again
		_, message, err = wsConn.ReadMessage()
		if err != nil {
			color.Red("Error reading search response after reconnection: %v", err)
			return
		}
	}

	var response map[string]interface{}
	err = json.Unmarshal(message, &response)
	if err != nil {
		color.Red("Error unmarshalling search response: %v", err)
		return
	}

	if event, ok := response["event"].(string); ok && event == "search_ack" {
		if searchID, ok := response["searchID"].(string); ok {
			GetFlags().SearchID = searchID
			if url, ok := response["url"].(string); ok {
				color.Green("Connected to GitHound Explore! View search results at: %s", url)
			}
		}
	} else if errorMsg, ok := response["error"].(string); ok {
		color.Red("Error starting search: %s", errorMsg)
	}

	// Set up a goroutine to handle sending results
	go func() {
		for {
			// Read any incoming messages to keep the connection alive
			_, message, err := wsConn.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					color.Red("WebSocket connection closed unexpectedly: %v", err)
					// Check if we should attempt reconnection
					if !handleWebSocketDisconnection(err) {
						return
					}
					continue
				}
				continue
			}

			// Handle any incoming messages if needed
			var msg map[string]interface{}
			if err := json.Unmarshal(message, &msg); err == nil {
				if event, ok := msg["event"].(string); ok {
					switch event {
					case "ping":
						if GetFlags().Debug {
							color.Red("[DEBUG] Sending pong WebSocket message")
						}
						err := wsConn.WriteMessage(websocket.TextMessage, []byte(`{"event": "pong"}`))
						if err != nil {
							color.Red("Error sending pong response: %v", err)
							// Check if connection is lost and handle reconnection
							if !handleWebSocketDisconnection(err) {
								return
							}
						}
					case "trufflehog_result":
						// Process trufflehog results
						if result, ok := msg["result"].(map[string]interface{}); ok {
							// Handle filesystem results
							if sourceMetadata, ok := result["SourceMetadata"].(map[string]interface{}); ok {
								if data, ok := sourceMetadata["Data"].(map[string]interface{}); ok {
									if filesystem, ok := data["Filesystem"].(map[string]interface{}); ok {
										if file, ok := filesystem["file"].(string); ok {
											// Extract filename from full path
											filename := filepath.Base(file)
											// Set repo to FILESYSTEM for filesystem results
											result["repo"] = "FILESYSTEM"
											result["filename"] = filename
										}
									}
								}
							}
							// Add the result to the search results
							resultJSON, err := json.Marshal(result)
							if err == nil {
								searchID := GetFlags().SearchID
								var resultPayload string
								if searchID != "" {
									resultPayload = fmt.Sprintf(`{"event": "search_result", "insertToken": "%s", "searchID": "%s", "result": %s}`, GetFlags().InsertKey, searchID, string(resultJSON))
								} else {
									resultPayload = fmt.Sprintf(`{"event": "search_result", "insertToken": "%s", "result": %s}`, GetFlags().InsertKey, string(resultJSON))
								}
								if GetFlags().Debug {
									color.Red("[DEBUG] Sending search result WebSocket message: %s", resultPayload)
								}
								SendMessageToWebSocket(resultPayload)
							}
						}
					}
				}
			}
		}
	}()
}
