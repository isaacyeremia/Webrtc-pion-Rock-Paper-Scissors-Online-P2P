package main

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v3"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// pesan signaling
type Signal struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// satu client = satu peer
type Client struct {
	ws *websocket.Conn
	pc *webrtc.PeerConnection
	dc *webrtc.DataChannel
}

// antrian matchmaking
var (
	waiting *Client
	mu      sync.Mutex
)

func wsHandler(w http.ResponseWriter, r *http.Request) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	// buat PeerConnection Pion
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		log.Println(err)
		return
	}

	client := &Client{ws: ws, pc: pc}

	// ICE dari server → client
	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		ws.WriteJSON(Signal{Type: "ice", Data: mustJSON(c.ToJSON())})
	})

	// DataChannel dari browser
	pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		client.dc = dc
		log.Println("DataChannel:", dc.Label())

		// relay data ke lawan
		dc.OnMessage(func(msg webrtc.DataChannelMessage) {
			mu.Lock()
			defer mu.Unlock()
			if opponent := getOpponent(client); opponent != nil && opponent.dc != nil {
				opponent.dc.Send(msg.Data)
			}
		})
	})

	// matchmaking
	mu.Lock()
	if waiting == nil {
		waiting = client
	} else {
		// pasangkan
		opponents[client] = waiting
		opponents[waiting] = client
		waiting = nil
	}
	mu.Unlock()

	// signaling loop
	for {
		var sig Signal
		if err := ws.ReadJSON(&sig); err != nil {
			break
		}

		switch sig.Type {
		case "offer":
			var offer webrtc.SessionDescription
			json.Unmarshal(sig.Data, &offer)
			client.pc.SetRemoteDescription(offer)

			answer, _ := client.pc.CreateAnswer(nil)
			client.pc.SetLocalDescription(answer)

			ws.WriteJSON(Signal{Type: "answer", Data: mustJSON(answer)})

		case "ice":
			var ice webrtc.ICECandidateInit
			json.Unmarshal(sig.Data, &ice)
			client.pc.AddICECandidate(ice)
		}
	}
}

var opponents = map[*Client]*Client{}

func getOpponent(c *Client) *Client {
	return opponents[c]
}

func mustJSON(v interface{}) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func main() {
	http.HandleFunc("/ws", wsHandler)
	log.Println("Pion MULTIPLAYER server running on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
