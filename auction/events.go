package auction

type Event interface {
	Event() string
}

type genericEvent struct {
	EventName string `json:"event"`
}

func (e genericEvent) Event() string {
	return e.EventName
}

type CloseItemEvent struct {
	ItemID string `json:"itemId"`
}

func (CloseItemEvent) Event() string {
	return "closeItem"
}

type OpenItemEvent struct {
	ItemID string `json:"itemId"`
}

func (OpenItemEvent) Event() string {
	return "openItem"
}

type BidEvent Bid

func (BidEvent) Event() string {
	return "bid"
}

type DeleteBidEvent struct {
	ItemID string `json:"itemId"`
	BidID string `json:"bidId"`
	BidCents int `json:"bid"`
	Bidder string `json:"bidder"`
	BidderDisplayName string `json:"bidderDisplayName"`
}

func (DeleteBidEvent) Event() string {
	return "deleteBid"
}