package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
)

var (
	clients = make(map[string]*Client)
	PORT    = 8000
	mut     = &sync.Mutex{}
	wg      = new(sync.WaitGroup)
)

type Client struct {
	writer  *http.ResponseWriter
	flusher *http.Flusher
}

type ReqBody struct {
	Message string `json:message`
	To      string `json:to`
}

func getClientId(r *http.Request) string {
	var id string
	cookie, err := r.Cookie("sse_client_id")
	if err != nil {
		if err == http.ErrNoCookie {
			id = uuid.New().String()
			return id
		}
	}

	return cookie.Value
}

func updateClient(clientId string, flusher *http.Flusher, writer *http.ResponseWriter, isListening bool) {
	mut.Lock()

	if !isListening {
		delete(clients, clientId)
		//fmt.Println("Deleted client - ", clientId, "\t Total connected clients - ", len(clients))
		mut.Unlock()
		return
	}

	clients[clientId] = &Client{
		writer:  writer,
		flusher: flusher,
	}
	mut.Unlock()
}

func getClient(clientId string) (*Client, bool) {
	client, ok := clients[clientId]

	if !ok {
		return nil, false
	}

	return client, true
}

func eventsHandlerfunc(w http.ResponseWriter, r *http.Request) {
	clientId := getClientId(r)

	switch r.Method {

	case http.MethodGet:
		// Required to listen to closes
		notify := r.Context().Done()
		// Create a flusher to flush the response writer buffer
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
			return
		}

		// save flusher and ResponseWrite for client
		updateClient(clientId, &flusher, &w, true)

		// Set headers for SSE
		fmt.Println("Stream joined by - ", clientId)
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		// Create a new cookie
		cookie := &http.Cookie{
			Name:     "sse_client_id",
			Value:    clientId,
			Path:     "/",
			Expires:  time.Now().Add(24 * time.Hour), // Set expiration time (24 hours from now)
			HttpOnly: true,                           // Optional: makes the cookie accessible only by the HTTP protocol
			Secure:   false,                          // Optional: only send the cookie over HTTPS connections (set to true in production)
		}

		// Set the cookie in the response
		http.SetCookie(w, cookie)

		fmt.Fprintf(w, "Welcome to the event stream - %s\n", clientId)
		flusher.Flush()

		for {
			select {
			case <-notify:
				// remove client from map
				// fmt.Printf("Client connection close - %s \n", clientId)
				updateClient(clientId, &flusher, &w, false)
				return
			}
		}

	case http.MethodPost:
		message := ReqBody{}
		// Decode JSON body into struct
		if err := json.NewDecoder(r.Body).Decode(&message); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		defer r.Body.Close()

		client, found := getClient(message.To)
		if !found {
			http.Error(w, "To client not found", http.StatusBadRequest)
			return
		}

		fmt.Fprintf((*client.writer), "%s \n", message.Message)
		(*client.flusher).Flush()

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}

}

func main() {
	wg.Add(1)
	go func() {
		http.HandleFunc("/events", eventsHandlerfunc)
		http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, "Serving")
		})
		http.HandleFunc("/render", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <title>SSE Client</title>
</head>
<body>
    <h1>Server-Sent Events Demo</h1>
    <div id="events"></div>

    <script>
        const eventSource = new EventSource('http://localhost:8000/events');

        eventSource.onmessage = function(e) {
		console.log(e.data)
            const eventDiv = document.getElementById('events');
            const p = document.createElement('p');
            p.textContent = e.data;
            eventDiv.appendChild(p);
        };

        eventSource.onerror = function(e) {
            console.error('SSE error:', e);
        };
    </script>
</body>
</html>
`)
		})
		http.ListenAndServe(":"+strconv.Itoa(PORT), nil)
	}()
	fmt.Println("SSE Server started on port : ", PORT)
	wg.Wait()
}
