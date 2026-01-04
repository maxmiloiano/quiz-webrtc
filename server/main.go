package main

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v3"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

/* ================= STRUCT ================= */

type Signal struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

type AnswerPayload struct {
	Type   string `json:"type"`
	ID     int    `json:"id"`
	Answer int    `json:"answer"`
	Time   int64  `json:"time"`
}

type Question struct {
	ID       int
	Text     string
	Options  []string
	Answer   int
	Duration int
}

type Player struct {
	DC    *webrtc.DataChannel
	Score int
}

type Room struct {
	sync.Mutex
	Players   map[string]*Player
	Questions []Question
	Current   int
}

/* ================= GLOBAL ROOM ================= */

var room = Room{
	Players: make(map[string]*Player),
	Questions: []Question{
		{1, "Apa kepanjangan CPU?", []string{
			"Central Processing Unit",
			"Computer Personal Unit",
			"Control Processing Unit",
		}, 0, 10},

		{2, "Apa fungsi RAM?", []string{
			"Penyimpanan sementara",
			"Penyimpanan permanen",
			"CPU processing",
		}, 0, 10},

		{3, "OS singkatan dari?", []string{
			"Operating System",
			"Open Software",
			"Optimal System",
		}, 0, 10},
	},
}

/* ================= MAIN ================= */

func main() {
	http.HandleFunc("/ws", handleWS)
	log.Println("🚀 Server running :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func handleWS(w http.ResponseWriter, r *http.Request) {
	conn, _ := upgrader.Upgrade(w, r, nil)
	defer conn.Close()

	pc, _ := webrtc.NewPeerConnection(webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{URLs: []string{"stun:stun.l.google.com:19302"}},
		},
	})
	defer pc.Close()

	playerID := r.RemoteAddr
	var questionStart time.Time

	pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		log.Println("New DataChannel:", dc.Label())

		room.Lock()
		room.Players[playerID] = &Player{DC: dc}
		room.Unlock()

		dc.OnOpen(func() {
			log.Println("👤 Player joined:", playerID)
			room.Current = 0
			room.Players[playerID].Score = 0
			sendQuestion(dc)
			questionStart = time.Now()
		})

		dc.OnMessage(func(msg webrtc.DataChannelMessage) {
			log.Println("From player:", string(msg.Data))

			var ans AnswerPayload
			json.Unmarshal(msg.Data, &ans)

			room.Lock()
			q := room.Questions[room.Current]

			if ans.Answer == q.Answer {
				room.Players[playerID].Score += 10
				log.Println("✅ Jawaban BENAR")
			} else {
				log.Println("❌ Jawaban SALAH")
			}

			score := room.Players[playerID].Score
			room.Current++
			room.Unlock()

			sendResult(dc, score, time.Since(questionStart).Milliseconds())

			time.Sleep(500 * time.Millisecond)

			sendQuestion(dc)
			questionStart = time.Now()
		})
	})

	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c != nil {
			b, _ := json.Marshal(c.ToJSON())
			conn.WriteJSON(Signal{Type: "candidate", Data: b})
		}
	})

	for {
		var sig Signal
		if conn.ReadJSON(&sig) != nil {
			return
		}

		if sig.Type == "offer" {
			var offer webrtc.SessionDescription
			json.Unmarshal(sig.Data, &offer)
			pc.SetRemoteDescription(offer)

			answer, _ := pc.CreateAnswer(nil)
			pc.SetLocalDescription(answer)
			b, _ := json.Marshal(answer)
			conn.WriteJSON(Signal{Type: "answer", Data: b})
		}

		if sig.Type == "candidate" {
			var c webrtc.ICECandidateInit
			json.Unmarshal(sig.Data, &c)
			pc.AddICECandidate(c)
		}
	}
}

/* ================= HELPERS ================= */

func sendQuestion(dc *webrtc.DataChannel) {
	room.Lock()
	defer room.Unlock()

	if room.Current >= len(room.Questions) {
		dc.SendText(`{"type":"end"}`)
		return
	}

	q := room.Questions[room.Current]

	payload := map[string]interface{}{
		"type":     "question",
		"id":       q.ID,
		"question": q.Text,
		"options":  q.Options,
		"duration": q.Duration,
		"progress": map[string]int{
			"current": room.Current + 1,
			"total":   len(room.Questions),
		},
	}

	b, _ := json.Marshal(payload)
	dc.SendText(string(b))
}

func sendResult(dc *webrtc.DataChannel, score int, time int64) {
	b, _ := json.Marshal(map[string]interface{}{
		"type":  "result",
		"score": score,
		"time":  time,
	})
	dc.SendText(string(b))
}
