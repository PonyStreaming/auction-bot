package api

import (
	"encoding/json"
	"fmt"
	"github.com/go-redis/redis/v7"
	"github.com/gorilla/mux"
	"log"
	"net/http"
	"time"

	"github.com/PonyFest/auction-bot/auction"
)

type APIServer struct {
	server *http.Server
	auction *auction.Auction
}

func New(auction *auction.Auction, password string) *APIServer {
	h := mux.NewRouter()
	a := &APIServer{
		auction: auction,
		server: &http.Server{
			Handler: basicAuth(acceptAllCors(h), password),
		},
	}
	h.HandleFunc("/api/openItem", a.handleOpenItem)
	h.HandleFunc("/api/events", a.handleEvents)
	h.HandleFunc("/api/closeItem", a.handleCloseItem)
	h.HandleFunc("/api/items", a.handleGetItems)
	h.HandleFunc("/api/currentItem", a.handleGetCurrentItem)
	h.HandleFunc("/api/items/{itemId}", a.handleItem)
	h.HandleFunc("/api/items/{itemId}/bids", a.handleItemBids)
	h.HandleFunc("/api/items/{itemId}/bids/{bidId}", a.handleSpecificBid)
	h.HandleFunc("/api/total", a.handleTotal)
	return a
}

func (a *APIServer) handleEvents(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	_, _ = w.Write([]byte(": hello\n\n"))
	w.(http.Flusher).Flush()

	ch := a.auction.Events()
	const pingTime = 45 * time.Second
	pingChannel := time.After(pingTime)
	for {
		output := ""
		select {
		case event := <-ch:
			data := map[string]interface{}{"type": event.Event(), "event": event}
			j, err := json.Marshal(data)
			if err != nil {
				break
			}
			output = fmt.Sprintf("data: %s\n\n", j)
		case <-pingChannel:
			pingChannel = time.After(pingTime)
			output = ": ping\n\n"
		}

		_, err := w.Write([]byte(output))
		if err == nil {
			w.(http.Flusher).Flush()
		} else {
			log.Printf("write failed, dropping connection: %v", err)
			break
		}
	}
}

func (a *APIServer) handleGetCurrentItem(w http.ResponseWriter, r *http.Request) {
	item := a.auction.CurrentItem()
	response := map[string]interface{}{
		"status": "ok",
		"item": item,
	}
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, fmt.Sprintf("encoding JSON failed: %v", err), http.StatusInternalServerError)
	}
}

func (a *APIServer) handleItem(w http.ResponseWriter, r *http.Request) {
	itemId := mux.Vars(r)["itemId"]
	item, err := a.auction.GetItem(itemId)
	if err != nil {
		if err == redis.Nil {
			http.Error(w, fmt.Sprintf("no such item: %q", itemId), http.StatusNotFound)
			return
		}
		http.Error(w, fmt.Sprintf("couldn't get item: %v", err), http.StatusInternalServerError)
		return
	}
	response := map[string]interface{}{
		"status": "ok",
		"item": item,
	}
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, fmt.Sprintf("encoding JSON failed: %v", err), http.StatusInternalServerError)
	}
}

func (a *APIServer) handleGetItems(w http.ResponseWriter, r *http.Request) {
	items, err := a.auction.GetItems()
	if err != nil {
		http.Error(w, fmt.Sprintf("couldn't get items: %v", err), http.StatusInternalServerError)
		return
	}
	response := map[string]interface{}{
		"status": "ok",
		"items": items,
	}
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, fmt.Sprintf("encoding JSON failed: %v", err), http.StatusInternalServerError)
	}
}

func (a *APIServer) handleItemBids(w http.ResponseWriter, r *http.Request) {
	itemId := mux.Vars(r)["itemId"]
	bids, err := a.auction.GetTopBids(itemId, 0)
	if err != nil {
		http.Error(w, fmt.Sprintf("couldn't look up bids: %v", err), http.StatusInternalServerError)
		return
	}
	output := map[string]interface{}{
		"status": "ok",
		"bids": bids,
	}
	if err := json.NewEncoder(w).Encode(output); err != nil {
		http.Error(w, fmt.Sprintf("couldn't encode bids: %v", err), http.StatusInternalServerError)
	}
}

func (a *APIServer) handleOpenItem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "bad method", http.StatusMethodNotAllowed)
		return
	}
	item := r.FormValue("itemId")
	if item == "" {
		http.Error(w, "no item specified", http.StatusBadRequest)
		return
	}
	if err := a.auction.OpenItem(item); err != nil {
		http.Error(w, fmt.Sprintf("could not open item: %v", err), http.StatusBadRequest)
		return
	}
	_, _ = w.Write([]byte(`{"status": "ok"}`))
}

func (a *APIServer) handleCloseItem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "bad method", http.StatusMethodNotAllowed)
		return
	}
	if err := a.auction.CloseItem(); err != nil {
		http.Error(w, fmt.Sprintf("closing item failed: %v.", err), http.StatusInternalServerError)
		return
	}
	_, _ = w.Write([]byte(`{"status": "ok"}`))
}

func (a *APIServer) handleSpecificBid(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "bad method", http.StatusMethodNotAllowed)
		return
	}
	vars := mux.Vars(r)
	itemId := vars["itemId"]
	bidId := vars["bidId"]
	if err := a.auction.DeleteBid(itemId, bidId); err != nil {
		http.Error(w, fmt.Sprintf("couldn't delete bid: %v", err), http.StatusInternalServerError)
	}
	_, _ = w.Write([]byte(`{"status": "ok"}`))
}

func (a *APIServer) handleTotal(w http.ResponseWriter, r *http.Request) {
	total := a.auction.TotalRaisedCents()
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "totalCents": total})
}

func (a *APIServer) ListenAndServe(addr string) error {
	a.server.Addr = addr
	return a.server.ListenAndServe()
}

