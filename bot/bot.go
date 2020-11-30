package bot

import (
	"fmt"
	"log"
	"math"
	"strconv"
	"strings"

	"github.com/PonyFest/auction-bot/auction"
	"github.com/bwmarrin/discordgo"
)

type AuctionBot struct {
	discord *discordgo.Session
	discordChannel string
	discordGuild string
	auction *auction.Auction
}

func New(auc *auction.Auction, discordToken, discordChannel string) (*AuctionBot, error) {
	d, err := discordgo.New("Bot " + discordToken)
	if err != nil {
		return nil, fmt.Errorf("couldn't create discord session: %v", err)
	}
	b := &AuctionBot{
		discord:        d,
		discordChannel: discordChannel,
		auction:        auc,
	}
	d.AddHandler(b.handleMessage)
	go b.handleAuctionUpdates()
	return b, nil
}

func (b *AuctionBot) RunForever() error {
	if err := b.discord.Open(); err != nil {
		log.Printf("Connecting to discord failed: %v.\n", err)
		return err
	}
	select{}
}

func (b *AuctionBot) handleAuctionUpdates() {
	ch := b.auction.Events()
	for {
		event, ok := <-ch
		log.Println(event, ok)
		if !ok {
			break
		}
		switch e := event.(type) {
		case *auction.CloseItemEvent:
			item, _ := b.auction.GetItem(e.ItemID)
			if item == nil {
				_, _ = b.discord.ChannelMessageSend(b.discordChannel, "Bidding on this item has closed.")
				break
			}
			bids, err := b.auction.GetTopBids(e.ItemID, 1)
			if err != nil {
				_, _ = b.discord.ChannelMessageSend(b.discordChannel, fmt.Sprintf("Bidding for **%s** has closed.", item.Title))
				break
			}
			if len(bids) == 0 {
				_, _ = b.discord.ChannelMessageSend(b.discordChannel, fmt.Sprintf("Bidding for **%s** has closed. There were no bids.", item.Title))
				break
			}
			_, _ = b.discord.ChannelMessageSend(b.discordChannel, fmt.Sprintf("Bidding for **%s** has closed. The winner was <@%s>, at $%d.%02d!", item.Title, bids[0].Bidder, bids[0].BidCents / 100, bids[0].BidCents % 100))
		case *auction.OpenItemEvent:
			item, _ := b.auction.GetItem(e.ItemID)
			if item == nil {
				_, _ = b.discord.ChannelMessageSend(b.discordChannel, "Bidding for the next item has started!")
				break
			}
			pictureURL := ""
			if len(item.Images) > 0 {
				pictureURL = "\n"+item.Images[0]
			}
			bids, _ := b.auction.GetTopBids(e.ItemID, 1)
			message := ""
			if len(bids) == 1 {
				message = fmt.Sprintf("Bidding for **%s** has reopened! The current high bid is **$%d.%02d**.\n\n%s%s", item.Title, bids[0].BidCents / 100, bids[0].BidCents % 100, item.Description, pictureURL)
			} else {
				message = fmt.Sprintf("Bidding for **%s** has started! Bidding starts at **$%d.%02d**.\n\n%s%s", item.Title, item.StartBid/100, item.StartBid%100, item.Description, pictureURL)
			}
			_, _ = b.discord.ChannelMessageSend(b.discordChannel, message)
		case *auction.DeleteBidEvent:
			topBids, err := b.auction.GetTopBids(e.ItemID, 1)
			if err != nil {
				return
			}
			currentItem := b.auction.CurrentItem()
			if currentItem == nil {
				return
			}
			if e.ItemID != currentItem.ID {
				return
			}
			if len(topBids) == 0 {
				message := fmt.Sprintf("<@%s>'s top bid of $%d.%02d has been rescinded. There are no longer any bids!", e.Bidder, e.BidCents / 100, e.BidCents % 100)
				_, _ = b.discord.ChannelMessageSend(b.discordChannel, message)
			} else if e.BidCents > topBids[0].BidCents {
				message := fmt.Sprintf("<@%s>'s top bid of $%d.%02d has been rescinded. The current top bid is **$%d.%02d** by <@%s>!", e.Bidder, e.BidCents / 100, e.BidCents % 100, topBids[0].BidCents / 100, topBids[0].BidCents % 100, topBids[0].Bidder)
				_, _ = b.discord.ChannelMessageSend(b.discordChannel, message)
			}
		}
	}
}

func (b *AuctionBot) handleMessage(s *discordgo.Session, m *discordgo.MessageCreate) {
	if s != b.discord {
		log.Println("Got a message from the wrong discord session???")
		return
	}
	if m.Author.ID == s.State.User.ID {
		return
	}
	if m.ChannelID != b.discordChannel {
		return
	}
	if !strings.HasPrefix(m.Content, "!") {
		return
	}
	b.processCommand(m)
}

func (b *AuctionBot) processCommand(m *discordgo.MessageCreate) {
	parts := strings.Split(strings.TrimSpace(m.Content[1:]), " ")
	command := parts[0]
	args := parts[1:]
	switch command {
	case "bid":
		b.handleBid(m, args)
	}
}


func (b *AuctionBot) handleBid(m *discordgo.MessageCreate, args []string) {
	currentItem := b.auction.CurrentItem()
	if currentItem == nil {
		_, _ = b.discord.ChannelMessageSend(m.ChannelID, "Nothing's up for auction right now.")
		return
	}
	if len(args) != 1 {
		_, _ = b.discord.ChannelMessageSend(m.ChannelID, "To bid, say `!bid price`, e.g. `!bid 50` to bid 50 dollars.")
		return
	}
	bidCents, err := strconv.ParseFloat(strings.TrimLeft(args[0], "$"), 64)
	if err != nil {
		_, _ = b.discord.ChannelMessageSend(m.ChannelID, fmt.Sprintf("%s, that was not a valid bid.", m.Author.Mention()))
		return
	}
	bidDollars := int(math.Round(bidCents * 100))
	nick := m.Author.Username
	if member, err := b.discord.GuildMember(m.GuildID, m.Author.ID); err == nil {
		if member.Nick != "" {
			nick = member.Nick
		}
	}
	if err := b.auction.Bid(bidDollars, m.Author.ID, nick); err != nil {
		_, _ = b.discord.ChannelMessageSend(m.ChannelID, fmt.Sprintf("%s, your bid failed: %v", m.Author.Mention(), err))
		return
	}
	_, _ = b.discord.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Thank you! The current high bid on **%s** is $%d.%02d, by %s.", currentItem.Title, bidDollars / 100, bidDollars % 100, m.Author.Mention()))
}
