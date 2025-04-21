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
var InsertKey string // Global variable for InsertKey

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

	// Use Insert Key from environment variable if available
	if GetFlags().InsertKey != "" {
		InsertKey = GetFlags().InsertKey
		return
	}

	// Fall back to reading from token file
	homeDir, err := os.UserHomeDir()
	if err != nil {
		color.Red("Error getting home directory: %v", err)
		time.Sleep(5 * time.Second)
		return
	}

	gitHoundDir := filepath.Join(homeDir, ".githound")
	tokenFilePath := filepath.Join(gitHoundDir, "insert_token.txt")

	var token string
	if _, err := os.Stat(tokenFilePath); err == nil {
		// Token file exists, load the token
		tokenBytes, err := ioutil.ReadFile(tokenFilePath)
		if err != nil {
			color.Red("Error accessing cached GitHound token at ~/.githound/insert_token.txt: %v", err)
			time.Sleep(5 * time.Second)
			return
		}
		token = string(tokenBytes)

		// Send the token to the WebSocket
		payload := fmt.Sprintf(`{"event": "gh_banner", "ghVersion": "1.0.0", "insertToken": "%s"}`, token)
		err = wsConn.WriteMessage(websocket.TextMessage, []byte(payload))
		if err != nil {
			color.Red("Error sending WebSocket message: %v", err)
			log.Fatal(err)
		}
		for {
			_, message, err := wsConn.ReadMessage()
			if err != nil {
				color.Red("Error reading WebSocket message: %v", err)
				fmt.Println(message)
				log.Fatal(err)
			}

			var response map[string]interface{}
			err = json.Unmarshal(message, &response)
			if err != nil {
				color.Red("Error unmarshalling WebSocket message: %v", err)
				log.Fatal(err)
			}

			if loggedIn, ok := response["logged_in"].(bool); ok && loggedIn {
				break
			} else {
				ConnectToAccount(response)
			}
		}
	} else {
		payload := fmt.Sprintf(`{"event": "gh_banner", "ghVersion": "1.0.0"}`)
		err = wsConn.WriteMessage(websocket.TextMessage, []byte(payload))
		if err != nil {
			color.Red("Error sending WebSocket message: %v", err)
			log.Fatal(err)
		}
		_, message, err := wsConn.ReadMessage()
		if err != nil {
			color.Red("Error reading WebSocket message: %v", err)
			log.Fatal(err)
		}

		var response map[string]interface{}
		_ = json.Unmarshal(message, &response)
		token = ConnectToAccount(response)
	}
	InsertKey = token
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
	authPayload := fmt.Sprintf(`{"event": "gh_banner", "ghVersion": "1.0.0", "insertToken": "%s"}`, InsertKey)
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
	payload := fmt.Sprintf(`{"event": "start_search", "insertToken": "%s", "searchQuery": %s}`, InsertKey, escapedQuery)
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
