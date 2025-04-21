package app

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	"github.com/fatih/color"
	"github.com/gorilla/websocket"
)

var wsConn *websocket.Conn
var wsMessageChannel chan string
var WsAuthenticated chan bool
var isAuthenticated bool

// var InsertKey string // Global variable for InsertKey

func StartWebSocket(url string) {
	WsAuthenticated = make(chan bool, 1) // Make channel buffered to prevent deadlock
	// color.Cyan("[*] Connecting to WebSocket at %s", url)
	dialer := websocket.Dialer{
		HandshakeTimeout:  10 * time.Second,
		EnableCompression: false,
	}
	var err error
	wsConn, _, err = dialer.Dial(url, nil)
	if err != nil {
		color.Red("Error connecting to GitHound Explore connector: %v", err)
		time.Sleep(5 * time.Second)
		WsAuthenticated <- false
		return
	}
	color.Green("[+] WebSocket connection established")
	wsMessageChannel = make(chan string)

	// Start a goroutine to handle messages from the channel
	go func() {
		for {
			select {
			case msg := <-wsMessageChannel:
				if wsConn == nil {
					color.Red("[DEBUG] WebSocket connection is nil, cannot send message")
					continue
				}
				err := wsConn.WriteMessage(websocket.TextMessage, []byte(msg))
				if err != nil {
					color.Red("[DEBUG] Error sending message from channel: %v", err)
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
	err = wsConn.WriteMessage(websocket.TextMessage, []byte(payload))
	if err != nil {
		color.Red("Error sending WebSocket message: %v", err)
		WsAuthenticated <- false
		return
	}

	// Handle initial response synchronously
	_, message, err := wsConn.ReadMessage()
	if err != nil {
		color.Red("Error reading initial WebSocket message: %v", err)
		WsAuthenticated <- false
		return
	}

	var response map[string]interface{}
	err = json.Unmarshal(message, &response)
	if err != nil {
		color.Red("Error unmarshalling initial WebSocket message: %v", err)
		WsAuthenticated <- false
		return
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
			return
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
					WsAuthenticated <- false
					return
				}

				var response map[string]interface{}
				if err := json.Unmarshal(message, &response); err != nil {
					color.Red("Error unmarshalling WebSocket message: %v", err)
					WsAuthenticated <- false
					return
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

	err := wsConn.WriteMessage(websocket.TextMessage, []byte(message))
	if err != nil {
		color.Red("Error sending WebSocket message: %v", err)
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
	err = wsConn.WriteMessage(websocket.TextMessage, []byte(payload))
	if err != nil {
		color.Red("Error sending search message: %v", err)
		return
	}

	_, message, err := wsConn.ReadMessage()
	if err != nil {
		color.Red("Error reading search response: %v", err)
		return
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
					return
				}
				continue
			}

			// Handle any incoming messages if needed
			var msg map[string]interface{}
			if err := json.Unmarshal(message, &msg); err == nil {
				if event, ok := msg["event"].(string); ok {
					switch event {
					case "ping":
						wsConn.WriteMessage(websocket.TextMessage, []byte(`{"event": "pong"}`))
					case "trufflehog_result":
						// Process trufflehog results
						if result, ok := msg["result"].(map[string]interface{}); ok {
							// Add the result to the search results
							resultJSON, err := json.Marshal(result)
							if err == nil {
								searchID := GetFlags().SearchID
								if searchID != "" {
									SendMessageToWebSocket(fmt.Sprintf(`{"event": "search_result", "insertToken": "%s", "searchID": "%s", "result": %s}`, GetFlags().InsertKey, searchID, string(resultJSON)))
								} else {
									SendMessageToWebSocket(fmt.Sprintf(`{"event": "search_result", "insertToken": "%s", "result": %s}`, GetFlags().InsertKey, string(resultJSON)))
								}
							}
						}
					}
				}
			}
		}
	}()
}
