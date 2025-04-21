package app

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/fatih/color"
	"github.com/gorilla/websocket"
)

var wsConn *websocket.Conn
var wsMessageChannel chan string

// var InsertKey string // Global variable for InsertKey

func StartWebSocket(url string) {
	dialer := websocket.Dialer{
		HandshakeTimeout:  10 * time.Second,
		EnableCompression: false,
	}
	var err error
	wsConn, _, err = dialer.Dial(url, nil)
	if err != nil {
		color.Red("Error connecting to GitHound Explore connector: %v", err)
		time.Sleep(5 * time.Second)
		return
	}
	wsMessageChannel = make(chan string)

	// Send initial message to start account linking
	var payload string
	if GetFlags().InsertKey != "" {
		payload = fmt.Sprintf(`{"event": "gh_banner", "ghVersion": "1.0.0", "insertToken": "%s"}`, GetFlags().InsertKey)
	} else {
		payload = fmt.Sprintf(`{"event": "gh_banner", "ghVersion": "1.0.0"}`)
	}
	err = wsConn.WriteMessage(websocket.TextMessage, []byte(payload))
	if err != nil {
		color.Red("Error sending WebSocket message: %v", err)
		return
	}

	// Handle initial response synchronously
	_, message, err := wsConn.ReadMessage()
	if err != nil {
		color.Red("Error reading initial WebSocket message: %v", err)
		return
	}

	var response map[string]interface{}
	err = json.Unmarshal(message, &response)
	if err != nil {
		color.Red("Error unmarshalling initial WebSocket message: %v", err)
		return
	}

	// If we have an insert token and the server confirms we're logged in, we're done
	if GetFlags().InsertKey != "" {
		if loggedIn, ok := response["logged_in"].(bool); ok && loggedIn {
			color.Green("[+] WebSocket connection established")

			// If in trufflehog mode, send start_search message
			if GetFlags().Trufflehog {
				escapedQuery, err := json.Marshal("TruffleHog Search")
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

				// Wait for search_ack response
				_, message, err := wsConn.ReadMessage()
				if err != nil {
					color.Red("Error reading search response: %v", err)
					return
				}

				var searchResponse map[string]interface{}
				err = json.Unmarshal(message, &searchResponse)
				if err != nil {
					color.Red("Error unmarshalling search response: %v", err)
					return
				}

				if event, ok := searchResponse["event"].(string); ok && event == "search_ack" {
					if searchID, ok := searchResponse["searchID"].(string); ok {
						GetFlags().SearchID = searchID
						if url, ok := searchResponse["url"].(string); ok {
							color.Green("Connected to GitHound Explore! View search results at: %s", url)
						}
					}
				} else if errorMsg, ok := searchResponse["error"].(string); ok {
					color.Red("Error starting search: %s", errorMsg)
				}
			}
		} else {
			color.Red("[!] Invalid insert token")
			return
		}
	} else {
		// Handle account linking URL
		if url, ok := response["url"].(string); ok {
			color.Cyan("Please visit the following URL to link your account: %s", url)
			color.Cyan("Waiting for verification...")
		}
	}

	// Start a goroutine to handle incoming messages
	go func() {
		defer wsConn.Close()
		for {
			_, message, err := wsConn.ReadMessage()
			if err != nil {
				color.Red("Error reading WebSocket message: %v", err)
				return
			}

			var response map[string]interface{}
			err = json.Unmarshal(message, &response)
			if err != nil {
				color.Red("Error unmarshalling WebSocket message: %v", err)
				continue
			}

			// Handle successful account linking
			if loggedIn, ok := response["logged_in"].(bool); ok && loggedIn {
				if insertToken, ok := response["insert_token"].(string); ok {
					GetFlags().InsertKey = insertToken

					// Save the token to file
					homeDir, err := os.UserHomeDir()
					if err != nil {
						color.Red("Error getting home directory: %v", err)
						continue
					}

					gitHoundDir := filepath.Join(homeDir, ".githound")
					tokenFilePath := filepath.Join(gitHoundDir, "insert_token.txt")

					// Create the .githound directory if it doesn't exist
					if _, err := os.Stat(gitHoundDir); os.IsNotExist(err) {
						err = os.MkdirAll(gitHoundDir, 0700)
						if err != nil {
							color.Red("Error creating .githound directory: %v", err)
							continue
						}
					}

					// Save the token to the file
					err = ioutil.WriteFile(tokenFilePath, []byte(insertToken), 0600)
					if err != nil {
						color.Red("Error writing token file: %v", err)
						continue
					}

					color.Green("[+] Account linked successfully!")
					color.Green("[+] Token saved to %s", tokenFilePath)
				}
			}
		}
	}()

	// Start a goroutine to handle outgoing messages
	go func() {
		defer wsConn.Close()
		for {
			message := <-wsMessageChannel
			err := wsConn.WriteMessage(websocket.TextMessage, []byte(message))
			if err != nil {
				color.Red("Error writing to WebSocket: %v", err)
				return
			}
		}
	}()
}

func ConnectToAccount(response map[string]interface{}) string {
	if wsConn == nil {
		color.Red("WebSocket connection is not established")
		return ""
	}
	var first = true
	var message string
	var token string
	for {
		if !first {
			_, message, err := wsConn.ReadMessage()
			if err != nil {
				color.Red("Error reading WebSocket message: %v", err)
				log.Fatal(err)
			}

			_ = json.Unmarshal(message, &response)
		} else {
			first = false
		}

		if loggedIn, ok := response["logged_in"].(bool); ok && !loggedIn {
			fmt.Println("login failed")
			if url, ok := response["url"].(string); ok {
				color.Cyan("Please visit the following URL to link your account: %s", url)
				color.Cyan("Waiting for verification...")
				for i := 0; i < 3; i++ {
					fmt.Print(".")
					time.Sleep(500 * time.Millisecond)
				}
				fmt.Println()
			}
		} else if loggedIn, ok := response["logged_in"].(bool); ok && loggedIn {
			if insertToken, ok := response["insert_token"].(string); ok {
				token = insertToken

				homeDir, err := os.UserHomeDir()
				if err != nil {
					color.Red("Error getting home directory: %v", err)
					log.Fatal(err)
				}

				gitHoundDir := filepath.Join(homeDir, ".githound")
				tokenFilePath := filepath.Join(gitHoundDir, "insert_token.txt")

				// Create the .githound directory if it doesn't exist
				if _, err := os.Stat(gitHoundDir); os.IsNotExist(err) {
					err = os.Mkdir(gitHoundDir, 0700)
					if err != nil {
						color.Red("Error creating .githound directory: %v", err)
						log.Fatal(err)
					}
				}

				// Save the token to the file
				err = ioutil.WriteFile(tokenFilePath, []byte(token), 0600)
				if err != nil {
					color.Red("Error writing token file: %v", err)
					log.Fatal(err)
				}

				break
			}
		} else {
			color.Red("Unexpected WebSocket response: %s", string(message))
			log.Fatal("Unexpected WebSocket response")
		}

	}

	return token
}

func SendMessageToWebSocket(message string) {
	if wsMessageChannel != nil {
		wsMessageChannel <- message
	}
}

// SendToWebSocket sends a message to the WebSocket connection
func SendToWebSocket(message string) {
	if wsMessageChannel != nil {
		wsMessageChannel <- message
	} else {
		color.Yellow("[!] WebSocket not initialized")
	}
}

func BrokerSearchCreation(query string) {
	if wsConn == nil {
		color.Red("WebSocket connection is not established")
		return
	}

	// First send the insert key to authenticate
	authPayload := fmt.Sprintf(`{"event": "gh_banner", "ghVersion": "1.0.0", "insertToken": "%s"}`, GetFlags().InsertKey)
	err := wsConn.WriteMessage(websocket.TextMessage, []byte(authPayload))
	if err != nil {
		color.Red("Error sending authentication message: %v", err)
		return
	}

	// Wait for authentication response
	_, message, err := wsConn.ReadMessage()
	if err != nil {
		color.Red("Error reading authentication response: %v", err)
		return
	}

	var authResponse map[string]interface{}
	err = json.Unmarshal(message, &authResponse)
	if err != nil {
		color.Red("Error unmarshalling authentication response: %v", err)
		return
	}

	if loggedIn, ok := authResponse["logged_in"].(bool); !ok || !loggedIn {
		color.Red("Error authenticating with insert key")
		return
	}

	// Skip start_search if in dashboard mode with search ID
	if GetFlags().Dashboard && GetFlags().SearchID != "" {
		color.Green("Connected to GitHound Explore! Using existing search ID: %s", GetFlags().SearchID)
		return
	}

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

	_, message, err = wsConn.ReadMessage()
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
		if _, ok := response["searchID"].(string); ok {
			if url, ok := response["url"].(string); ok {
				color.Green("Connected to GitHound Explore! View search results at: %s", url)
			}
		}
	} else if errorMsg, ok := response["error"].(string); ok {
		color.Red("Error starting search: %s", errorMsg)
	}
}
