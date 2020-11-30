package auction

import (
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"log"
	"strconv"

	"github.com/go-redis/redis/v7"
)

const auctionUpdatesKey = "auction-updates"
const currentItemKey = "current-item"
const allItemsKey = "all-items"
const totalRaisedKey = "total-raised"

type Auction struct {
	redis *redis.Client
	pubsubs []*redis.PubSub
}

type Item struct {
	Title string `json:"title"`
	Description string `json:"description"`
	Images []string `json:"images"`
	StartBid int `json:"startBid"`
	Closed bool `json:"closed"`
	ID string `json:"id"`
	Donator string `json:"donator"`
	Country string `json:"country"`
}

type Bid struct {
	BidCents int `json:"bid"`
	Bidder string `json:"bidder"`
	BidderDisplayName string `json:"bidderDisplayName"`
	ID string `json:"id"`
	ItemID string `json:"itemId"`
}

func New(redis *redis.Client) *Auction {
	return &Auction{
		redis: redis,
	}
}

func (a *Auction) Close() {
	for _, pubsub := range a.pubsubs {
		_ = pubsub.Close()
	}
}

func (a *Auction) CurrentItem() *Item {
	itemID, err := a.redis.Get(currentItemKey).Result()
	if err != nil {
		return nil
	}
	if itemID == "" {
		return nil
	}
	item, err := a.GetItem(itemID)
	if err != nil {
		return nil
	}
	return item
}

func (a *Auction) GetItems() ([]Item, error) {
	itemIDs := a.redis.SMembers(allItemsKey).Val()
	itemBlobs := a.redis.MGet(itemIDs...).Val()
	items := make([]Item, 0, len(itemBlobs))
	for i, b := range itemBlobs {
		var item Item
		if err := json.Unmarshal([]byte(b.(string)), &item); err != nil {
			continue
		}
		item.ID = itemIDs[i]
		items = append(items, item)
	}
	return items, nil
}

func (a *Auction) GetItem(itemID string) (*Item, error) {
	itemJSON, err := a.redis.Get(itemID).Result()
	if err != nil {
		return nil, err
	}
	var item Item
	if err := json.Unmarshal([]byte(itemJSON), &item); err != nil {
		return nil, err
	}
	item.ID = itemID
	return &item, nil
}

// GetTopBids returns the top bids.
// if bids is positive, it returns that many bids
// if bids is zero, is returns all bids
// if bids is negative, the behaviour is undefined
func (a *Auction) GetTopBids(itemID string, bids int) ([]Bid, error) {
	bids *= -1
	result, err := a.redis.LRange("bids-" + itemID, int64(bids), -1).Result()
	if err != nil {
		return nil, err
	}
	ret := make([]Bid, 0, len(result))
	for _, bidThing := range result {
		var bid Bid
		if err := json.Unmarshal([]byte(bidThing), &bid); err != nil {
			continue
		}
		ret = append(ret, bid)
	}
	return ret, nil
}

func (a *Auction) Bid(cents int, bidder string, displayName string) error {
	itemID, err := a.redis.Get(currentItemKey).Result()
	if err != nil {
		return nil
	}
	s := `
local bid = tonumber(ARGV[1])
local bidKey = KEYS[1]
local auctionUpdatesKey = KEYS[2]
local bidder = ARGV[2]
local bidderDisplayName = ARGV[3]
local bidId = ARGV[4]
local itemId = ARGV[5]
local currentBidInfo = redis.call("LRANGE", bidKey, -1, -1)
if table.getn(currentBidInfo) > 0 then
	local currentBid = cjson.decode(currentBidInfo[1])["bid"]
	if currentBid + 100 > bid then
		return redis.error_reply(string.format("you must bid at least $1 more than the previous high bid of $%d.%02d", currentBid / 100, currentBid % 100))
	end
end
redis.call("RPUSH", bidKey, cjson.encode({bid=bid, bidder=bidder, bidderDisplayName=bidderDisplayName, id=bidId, itemId=itemId}))
redis.call("PUBLISH", auctionUpdatesKey, cjson.encode({event="bid", bid=bid, bidder=bidder, bidderDisplayName=bidderDisplayName, id=bidId, itemId=itemId}))
return redis.status_reply("ok")`
	script := redis.NewScript(s)
	bidId := uuid.New()
	if err := script.Run(a.redis, []string{"bids-" + itemID, auctionUpdatesKey}, strconv.Itoa(cents), bidder, displayName, bidId.String(), itemID).Err(); err != nil {
		return err
	}
	return nil
}

func (a *Auction) OpenItem(itemId string) error {
	item, err := a.GetItem(itemId)
	if err != nil {
		return err
	}
	if item == nil {
		return fmt.Errorf("no item with ID %q", itemId)
	}
	if item.Closed {
		bids, _ := a.GetTopBids(itemId, 1)
		if len(bids) == 1 {
			a.redis.DecrBy(totalRaisedKey, int64(bids[0].BidCents))
		}
	}
	if err := a.redis.Set(currentItemKey, itemId, 0).Err(); err != nil {
		return fmt.Errorf("couldn't set current-item: %v", err)
	}
	return a.redis.Publish(auctionUpdatesKey, `{"event": "openItem", "itemId": "` + itemId + `"}`).Err()
}

func (a *Auction) CloseItem() error {
	currentItem, err := a.redis.Get(currentItemKey).Result()
	if err != nil {
		return err
	}
	s := `
local key = KEYS[1]
local json = cjson.decode(redis.call("GET", key))
json.closed = true
redis.call("SET", key, cjson.encode(json))
return redis.status_reply("OK")
`
	script := redis.NewScript(s)
	if err := script.Run(a.redis, []string{currentItem}).Err(); err != nil {
		return fmt.Errorf("failed to update closed status: %v", err)
	}
	if err := a.redis.Set(currentItemKey, "", 0).Err(); err != nil {
		return err
	}
	highestBids, _ := a.GetTopBids(currentItem, 1)
	if len(highestBids) == 1 {
		if err := a.redis.IncrBy(totalRaisedKey, int64(highestBids[0].BidCents)).Err(); err != nil {
			// failing here would be confusing?
			// really this should all be an atomic operation.
			//return err
		}
	}
	return a.redis.Publish(auctionUpdatesKey, `{"event": "closeItem", "itemId": "` + currentItem + `"}`).Err()
}

func (a *Auction) DeleteBid(itemId, bidId string) error {
	s := `
local bidsKey = KEYS[1]
local currentItemKey = KEYS[2]
local totalRaisedKey = KEYS[3]
local auctionUpdatesKey = KEYS[4]
local itemId = ARGV[1]
local bidId = ARGV[2]
local bids = redis.call("LRANGE", bidsKey, 0, -1)
for i, bid in ipairs(bids) do
	local bidInfo = cjson.decode(bid)
	if bidInfo.id == bidId then
		redis.call("LREM", bidsKey, 0, bid)
		-- if i == table.getn(bids) then
		-- 	if redis.call("GET", currentItemKey) ~= itemId then
		--		redis.call("DECRBY", totalRaisedKey, bidInfo.bid)
		--	end
		-- end
		redis.call("PUBLISH", auctionUpdatesKey, cjson.encode({event="deleteBid", itemId=itemId, bidId=bidId, bid=bidInfo.bid, bidder=bidInfo.bidder, bidderDisplayName=bidInfo.bidderDisplayName}))
		return redis.status_reply("ok")
	end
end
return redis.error_reply("no such bid exists")
`
	script := redis.NewScript(s)
	if err := script.Run(a.redis, []string{"bids-" + itemId, currentItemKey, totalRaisedKey, auctionUpdatesKey}, itemId, bidId).Err(); err != nil {
		return err
	}
	return nil
}

func (a *Auction) TotalRaisedCents() int {
	raisedString := a.redis.Get(totalRaisedKey).Val()
	if raisedString == "" {
		return 0
	}
	raisedCents, err := strconv.Atoi(raisedString)
	if err != nil {
		return 0
	}
	return raisedCents
}

// Events returns a channel that will receive auction event updates.
// If called repeatedly, it will return a new channel each time. Each channel will
// receive every event after it is created.
func (a *Auction) Events() <-chan Event {
	pubsub := a.redis.Subscribe(auctionUpdatesKey)
	a.pubsubs = append(a.pubsubs, pubsub)
	ch := pubsub.Channel()
	out := make(chan Event)
	go func() {
		for {
			message, ok := <-ch
			log.Println(message, ok)
			if !ok {
				break
			}
			var e genericEvent
			if err := json.Unmarshal([]byte(message.Payload), &e); err != nil {
				continue
			}
			var what Event
			switch e.Event() {
			case "openItem":
				what = &OpenItemEvent{}
			case "closeItem":
				what = &CloseItemEvent{}
			case "bid":
				what = &BidEvent{}
			case "deleteBid":
				what = &DeleteBidEvent{}
			}
			if what == nil {
				continue
			}
			if err := json.Unmarshal([]byte(message.Payload), what); err != nil {
				log.Printf("Couldn't unmarshal event: %v.\n", err)
				continue
			}
			out <- what
		}
		close(out)
	}()
	return out
}
